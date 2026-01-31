import asyncio
import logging
from typing import Union

import ccxt
import ccxt.async_support as ccxt_async
import ccxt.pro as ccxtpro
import grpc
from google.protobuf.timestamp_pb2 import Timestamp
from google.type import decimal_pb2

from opensqt.market_maker.v1 import (
    events_pb2,
    exchange_pb2,
    exchange_pb2_grpc,
    types_pb2,
)
from opensqt.market_maker.v1 import resources_pb2 as models_pb2

from .errors import handle_ccxt_exception, retry_transient

logger = logging.getLogger(__name__)


class BinanceConnector(exchange_pb2_grpc.ExchangeServiceServicer):
    def __init__(self, api_key: str, secret_key: str, exchange_type: str = "futures"):
        self.api_key = api_key
        self.secret_key = secret_key
        self.exchange_type = exchange_type

        options = {}
        if exchange_type == "futures":
            options["defaultType"] = "future"

        params = {
            "apiKey": api_key,
            "secret": secret_key,
            "options": options,
            "enableRateLimit": True,
        }
        self.exchange = ccxt_async.binance(params)
        self.exchange_pro = ccxtpro.binance(params)
        self._markets_loaded = False
        self._market_lock = asyncio.Lock()

    async def stop(self):
        await self.exchange.close()
        await self.exchange_pro.close()

    async def _ensure_markets(self):
        if not self._markets_loaded:
            async with self._market_lock:
                if not self._markets_loaded:
                    await self.exchange.load_markets()
                    self._markets_loaded = True

    async def _get_name_impl(
        self, req: exchange_pb2.GetNameRequest
    ) -> exchange_pb2.GetNameResponse:
        return exchange_pb2.GetNameResponse(name="binance")

    @handle_ccxt_exception
    @retry_transient()
    async def GetName(self, request, context):
        return await self._get_name_impl(request)

    async def _get_type_impl(
        self, req: exchange_pb2.GetTypeRequest
    ) -> exchange_pb2.GetTypeResponse:
        extype = (
            types_pb2.EXCHANGE_TYPE_FUTURES
            if self.exchange_type == "futures"
            else types_pb2.EXCHANGE_TYPE_SPOT
        )
        return exchange_pb2.GetTypeResponse(type=extype, is_unified_margin=False)

    @handle_ccxt_exception
    @retry_transient()
    async def GetType(self, request, context):
        return await self._get_type_impl(request)

    async def _get_latest_price_impl(
        self, req: exchange_pb2.GetLatestPriceRequest
    ) -> exchange_pb2.GetLatestPriceResponse:
        symbol = req.symbol
        ticker = await self.exchange.fetch_ticker(symbol)
        return exchange_pb2.GetLatestPriceResponse(
            price=self._to_decimal(ticker["last"])
        )

    @handle_ccxt_exception
    @retry_transient()
    async def GetLatestPrice(self, request, context):
        return await self._get_latest_price_impl(request)

    async def _get_symbol_info_impl(
        self, req: exchange_pb2.GetSymbolInfoRequest
    ) -> models_pb2.SymbolInfo:
        symbol = req.symbol
        await self._ensure_markets()
        market = self.exchange.market(symbol)

        tick_size = "0"
        step_size = "0"

        if "info" in market and "filters" in market["info"]:
            for f in market["info"]["filters"]:
                if f["filterType"] == "PRICE_FILTER":
                    tick_size = f.get("tickSize", "0")
                if f["filterType"] == "LOT_SIZE":
                    step_size = f.get("stepSize", "0")

        return models_pb2.SymbolInfo(
            symbol=symbol,
            price_precision=market["precision"].get("price", 8),
            quantity_precision=market["precision"].get("amount", 8),
            base_asset=market["base"],
            quote_asset=market["quote"],
            min_quantity=self._to_decimal(market["limits"]["amount"].get("min")),
            min_notional=self._to_decimal(market["limits"]["cost"].get("min")),
            tick_size=decimal_pb2.Decimal(value=tick_size),
            step_size=decimal_pb2.Decimal(value=step_size),
        )

    @handle_ccxt_exception
    @retry_transient()
    async def GetSymbolInfo(self, request, context):
        return await self._get_symbol_info_impl(request)

    async def _place_order_impl(
        self, req: models_pb2.PlaceOrderRequest
    ) -> models_pb2.Order:
        symbol = req.symbol
        side = self._reverse_map_side(req.side)
        order_type = self._reverse_map_type(req.type)
        amount = req.quantity.value
        price = req.price.value if req.price and req.price.value else None

        params = {}
        if req.client_order_id:
            params["clientOrderId"] = req.client_order_id
        if req.post_only:
            params["timeInForce"] = "GTX"
        if req.reduce_only:
            params["reduceOnly"] = True
        if req.use_margin:
            params["margin"] = True

        try:
            order = await self.exchange.create_order(
                symbol, order_type, side, amount, price, params
            )
        except ccxt.DuplicateOrderId:
            if req.client_order_id:
                try:
                    # Binance usually expects origClientOrderId for lookups
                    order = await self.exchange.fetch_order(
                        None, symbol, {"clientOrderId": req.client_order_id}
                    )
                except Exception as e:
                    logger.error(
                        f"Failed to fetch existing order {req.client_order_id}: {e}"
                    )
                    raise
            else:
                raise

        return self._map_order(order)

    @handle_ccxt_exception
    @retry_transient()
    async def PlaceOrder(self, request, context):
        return await self._place_order_impl(request)

    @handle_ccxt_exception
    @retry_transient()
    async def BatchPlaceOrders(self, request, context):
        if self.exchange.has.get("createOrders"):
            ccxt_orders = []
            for req in request.orders:
                symbol = req.symbol
                side = self._reverse_map_side(req.side)
                order_type = self._reverse_map_type(req.type)
                amount = req.quantity.value
                price = req.price.value if req.price and req.price.value else None
                params = {}
                if req.client_order_id:
                    params["clientOrderId"] = req.client_order_id
                if req.post_only:
                    params["timeInForce"] = "GTX"
                if req.reduce_only:
                    params["reduceOnly"] = True

                ccxt_orders.append(
                    {
                        "symbol": symbol,
                        "type": order_type,
                        "side": side,
                        "amount": amount,
                        "price": price,
                        "params": params,
                    }
                )

            try:
                orders = await self.exchange.create_orders(ccxt_orders)
                response_orders = [self._map_order(o) for o in orders]
                return exchange_pb2.BatchPlaceOrdersResponse(
                    orders=response_orders, all_success=True
                )
            except Exception as e:
                logger.warning(
                    f"Batch createOrders failed, falling back to sequential: {e}"
                )

        # Parallel execution fallback using internal impl to avoid RPC-wide aborts
        tasks = [self._place_order_impl(req) for req in request.orders]
        results = await asyncio.gather(*tasks, return_exceptions=True)

        response_orders = []
        errors = []
        all_success = True

        for i, res in enumerate(results):
            if isinstance(res, Exception):
                logger.error(f"Batch order component failure at index {i}: {res}")
                all_success = False
                errors.append(
                    exchange_pb2.BatchOrderError(
                        index=i,
                        client_order_id=request.orders[i].client_order_id,
                        error_message=str(res),
                        code=self._map_exception_to_code(res),
                    )
                )
            else:
                response_orders.append(res)

        return exchange_pb2.BatchPlaceOrdersResponse(
            orders=response_orders, all_success=all_success, errors=errors
        )

    def _map_exception_to_code(self, e: Exception) -> int:
        from .errors import EXCEPTION_MAP

        for exc_class, code in EXCEPTION_MAP:
            if isinstance(e, exc_class):
                return code.value[0] if isinstance(code.value, tuple) else code.value
        return (
            grpc.StatusCode.UNKNOWN.value[0]
            if isinstance(grpc.StatusCode.UNKNOWN.value, tuple)
            else grpc.StatusCode.UNKNOWN.value
        )

    async def _cancel_order_impl(
        self, req: exchange_pb2.CancelOrderRequest
    ) -> exchange_pb2.CancelOrderResponse:
        await self.exchange.cancel_order(str(req.order_id), req.symbol)
        return exchange_pb2.CancelOrderResponse()

    @handle_ccxt_exception
    @retry_transient()
    async def CancelOrder(self, request, context):
        return await self._cancel_order_impl(request)

    @handle_ccxt_exception
    @retry_transient()
    async def BatchCancelOrders(self, request, context):
        symbol = request.symbol
        order_ids = request.order_ids

        if self.exchange.has.get("cancelOrders"):
            # CCXT cancel_orders usually takes (ids, symbol, params)
            ids = [str(oid) for oid in order_ids]
            await self.exchange.cancel_orders(ids, symbol)
            return exchange_pb2.BatchCancelOrdersResponse()
        else:
            # Parallel execution fallback using internal impl to avoid RPC-wide aborts
            tasks = [
                self._cancel_order_impl(
                    exchange_pb2.CancelOrderRequest(symbol=symbol, order_id=oid)
                )
                for oid in order_ids
            ]
            results = await asyncio.gather(*tasks, return_exceptions=True)

            errors = []
            for i, res in enumerate(results):
                if isinstance(res, Exception):
                    logger.error(f"Batch cancel component failure at index {i}: {res}")
                    errors.append(
                        exchange_pb2.BatchOrderError(
                            index=i,
                            error_message=str(res),
                            code=self._map_exception_to_code(res),
                        )
                    )

            return exchange_pb2.BatchCancelOrdersResponse(errors=errors)

    async def _get_order_impl(
        self, req: exchange_pb2.GetOrderRequest
    ) -> models_pb2.Order:
        order = await self.exchange.fetch_order(str(req.order_id), req.symbol)
        return self._map_order(order)

    @handle_ccxt_exception
    @retry_transient()
    async def GetOrder(self, request, context):
        return await self._get_order_impl(request)

    async def _get_open_orders_impl(
        self, req: exchange_pb2.GetOpenOrdersRequest
    ) -> exchange_pb2.GetOpenOrdersResponse:
        orders = await self.exchange.fetch_open_orders(req.symbol)
        response_orders = [self._map_order(o) for o in orders]
        return exchange_pb2.GetOpenOrdersResponse(orders=response_orders)

    @handle_ccxt_exception
    @retry_transient()
    async def GetOpenOrders(self, request, context):
        return await self._get_open_orders_impl(request)

    async def _get_account_impl(
        self, req: exchange_pb2.GetAccountRequest
    ) -> models_pb2.Account:
        balance = await self.exchange.fetch_balance()
        total_wallet = balance.get("total", {}).get("USDT", 0)
        available = balance.get("free", {}).get("USDT", 0)

        positions_resp = await self._get_positions_impl(
            exchange_pb2.GetPositionsRequest()
        )

        return models_pb2.Account(
            total_wallet_balance=self._to_decimal(total_wallet),
            total_margin_balance=self._to_decimal(total_wallet),
            available_balance=self._to_decimal(available),
            positions=positions_resp.positions,
            account_leverage=10,
        )

    @handle_ccxt_exception
    @retry_transient()
    async def GetAccount(self, request, context):
        return await self._get_account_impl(request)

    async def _get_positions_impl(
        self, req: exchange_pb2.GetPositionsRequest
    ) -> exchange_pb2.GetPositionsResponse:
        symbol = req.symbol
        positions = await self.exchange.fetch_positions()
        if symbol:
            positions = [p for p in positions if p["symbol"] == symbol]

        res = []
        for p in positions:
            contracts = p.get("contracts") or p.get("size") or 0
            if float(contracts) == 0:
                continue

            res.append(
                models_pb2.Position(
                    symbol=p["symbol"],
                    size=self._to_decimal(contracts),
                    entry_price=self._to_decimal(p.get("entryPrice", 0)),
                    mark_price=self._to_decimal(p.get("markPrice", 0)),
                    unrealized_pnl=self._to_decimal(p.get("unrealizedPnl", 0)),
                    leverage=int(p.get("leverage", 1)),
                    margin_type=p.get("marginMode", "cross"),
                    liquidation_price=self._to_decimal(p.get("liquidationPrice", 0)),
                )
            )
        return exchange_pb2.GetPositionsResponse(positions=res)

    @handle_ccxt_exception
    @retry_transient()
    async def GetPositions(self, request, context):
        return await self._get_positions_impl(request)

    async def _get_symbols_impl(
        self, req: exchange_pb2.GetSymbolsRequest
    ) -> exchange_pb2.GetSymbolsResponse:
        await self._ensure_markets()
        return exchange_pb2.GetSymbolsResponse(
            symbols=list(self.exchange.markets.keys())
        )

    @handle_ccxt_exception
    @retry_transient()
    async def GetSymbols(self, request, context):
        return await self._get_symbols_impl(request)

    async def _get_funding_rate_impl(
        self, req: exchange_pb2.GetFundingRateRequest
    ) -> models_pb2.FundingRate:
        symbol = req.symbol
        rate = await self.exchange.fetch_funding_rate(symbol)
        return models_pb2.FundingRate(
            exchange="binance",
            symbol=symbol,
            rate=self._to_decimal(rate["fundingRate"]),
            next_funding_time=int(rate.get("nextFundingTime", 0)),
            timestamp=int(rate.get("timestamp", 0)),
        )

    @handle_ccxt_exception
    @retry_transient()
    async def GetFundingRate(self, request, context):
        return await self._get_funding_rate_impl(request)

    async def _get_funding_rates_impl(
        self, req: exchange_pb2.GetFundingRatesRequest
    ) -> exchange_pb2.GetFundingRatesResponse:
        rates = await self.exchange.fetch_funding_rates()
        res = []
        for symbol, rate in rates.items():
            res.append(
                models_pb2.FundingRate(
                    exchange="binance",
                    symbol=symbol,
                    rate=self._to_decimal(rate["fundingRate"]),
                    next_funding_time=int(rate.get("nextFundingTime", 0)),
                    timestamp=int(rate.get("timestamp", 0)),
                )
            )
        return exchange_pb2.GetFundingRatesResponse(rates=res)

    @handle_ccxt_exception
    @retry_transient()
    async def GetFundingRates(self, request, context):
        return await self._get_funding_rates_impl(request)

    async def _get_tickers_impl(
        self, req: exchange_pb2.GetTickersRequest
    ) -> exchange_pb2.GetTickersResponse:
        tickers = await self.exchange.fetch_tickers()
        res = []
        for symbol, t in tickers.items():
            res.append(
                models_pb2.Ticker(
                    symbol=symbol,
                    price_change=self._to_decimal(t.get("change", 0)),
                    price_change_percent=self._to_decimal(
                        (t.get("percentage", 0) or 0) / 100.0
                    ),
                    last_price=self._to_decimal(t.get("last", 0)),
                    volume=self._to_decimal(t.get("baseVolume", 0)),
                    quote_volume=self._to_decimal(t.get("quoteVolume", 0)),
                    timestamp=int(t.get("timestamp", 0)),
                )
            )
        return exchange_pb2.GetTickersResponse(tickers=res)

    @handle_ccxt_exception
    @retry_transient()
    async def GetTickers(self, request, context):
        return await self._get_tickers_impl(request)

    async def SubscribePrice(self, request, context):
        symbols = request.symbols
        if not symbols:
            return

        while True:
            try:
                # CCXT Pro watch_tickers is more efficient for multiple symbols
                tickers = await self.exchange_pro.watch_tickers(symbols)
                for symbol in symbols:
                    if symbol in tickers:
                        ticker = tickers[symbol]
                        price_change = events_pb2.PriceChange(
                            symbol=ticker["symbol"],
                            price=self._to_decimal(ticker["last"]),
                            timestamp=Timestamp(),
                        )
                        price_change.timestamp.FromMilliseconds(ticker["timestamp"])
                        yield price_change
            except asyncio.CancelledError:
                break
            except Exception as e:
                logger.error(f"Error in SubscribePrice: {e}")
                await asyncio.sleep(5)

    async def SubscribeOrders(self, request, context):
        while True:
            try:
                orders = await self.exchange_pro.watch_orders()
                for order in orders:
                    yield self._map_order_update(order)
            except asyncio.CancelledError:
                break
            except Exception as e:
                logger.error(f"Error in SubscribeOrders: {e}")
                await asyncio.sleep(5)

    async def SubscribeKlines(self, request, context):
        symbols = request.symbols
        interval = request.interval

        async def watch_symbol(symbol):
            while True:
                try:
                    ohlcvs = await self.exchange_pro.watch_ohlcv(symbol, interval)
                    if ohlcvs:
                        last = ohlcvs[-1]
                        yield models_pb2.Candle(
                            symbol=symbol,
                            open=self._to_decimal(last[1]),
                            high=self._to_decimal(last[2]),
                            low=self._to_decimal(last[3]),
                            close=self._to_decimal(last[4]),
                            volume=self._to_decimal(last[5]),
                            timestamp=int(last[0]),
                            is_closed=False,
                        )
                except asyncio.CancelledError:
                    break
                except Exception as e:
                    logger.error(f"Error watching Klines for {symbol}: {e}")
                    await asyncio.sleep(5)

        # Merge streams with bounded queue
        queue = asyncio.Queue(maxsize=100)

        async def producer(gen):
            try:
                async for item in gen:
                    await queue.put(item)
            except asyncio.CancelledError:
                pass
            except Exception as e:
                logger.error(f"Producer error: {e}")

        generators = [watch_symbol(s) for s in symbols]
        producers = [asyncio.create_task(producer(gen)) for gen in generators]

        try:
            while True:
                item = await queue.get()
                yield item
        finally:
            for p in producers:
                if not p.done():
                    p.cancel()
            if producers:
                await asyncio.gather(*producers, return_exceptions=True)

    async def SubscribeAccount(self, request, context):
        while True:
            try:
                balance = await self.exchange_pro.watch_balance()
                total_wallet = balance.get("total", {}).get("USDT", 0)
                available = balance.get("free", {}).get("USDT", 0)

                yield models_pb2.Account(
                    total_wallet_balance=self._to_decimal(total_wallet),
                    total_margin_balance=self._to_decimal(total_wallet),
                    available_balance=self._to_decimal(available),
                    positions=[],
                    account_leverage=10,
                )
            except asyncio.CancelledError:
                break
            except Exception as e:
                logger.error(f"Error in SubscribeAccount: {e}")
                await asyncio.sleep(5)

    async def SubscribePositions(self, request, context):
        filter_symbol = request.symbol
        while True:
            try:
                positions = await self.exchange_pro.watch_positions()
                for pos in positions:
                    if filter_symbol and pos["symbol"] != filter_symbol:
                        continue

                    yield models_pb2.Position(
                        symbol=pos["symbol"],
                        size=self._to_decimal(pos.get("contracts", 0)),
                        entry_price=self._to_decimal(pos.get("entryPrice", 0)),
                        mark_price=self._to_decimal(pos.get("markPrice", 0)),
                        unrealized_pnl=self._to_decimal(pos.get("unrealizedPnl", 0)),
                        leverage=int(pos.get("leverage", 1)),
                        margin_type=pos.get("marginType", "cross"),
                        isolated_margin=self._to_decimal(pos.get("isolatedWallet", 0)),
                    )
            except asyncio.CancelledError:
                break
            except Exception as e:
                logger.error(f"Error in SubscribePositions: {e}")
                await asyncio.sleep(5)

    def _map_order_update(self, order):
        return events_pb2.OrderUpdate(
            order_id=int(order["id"]) if order["id"].isdigit() else 0,
            client_order_id=order.get("clientOrderId", ""),
            symbol=order["symbol"],
            side=self._map_side(order["side"]),
            type=self._map_type(order["type"]),
            status=self._map_status(order["status"]),
            price=self._to_decimal(order.get("price", 0)),
            quantity=self._to_decimal(order.get("amount", 0)),
            executed_qty=self._to_decimal(order.get("filled", 0)),
            avg_price=self._to_decimal(order.get("average", 0)),
            update_time=int(order.get("timestamp", 0)),
        )

    def _map_side(self, side):
        s = (side or "").lower()
        if s == "buy":
            return types_pb2.ORDER_SIDE_BUY
        if s == "sell":
            return types_pb2.ORDER_SIDE_SELL
        return types_pb2.ORDER_SIDE_UNSPECIFIED

    def _map_type(self, order_type):
        t = (order_type or "").lower()
        if t == "limit":
            return types_pb2.ORDER_TYPE_LIMIT
        if t == "market":
            return types_pb2.ORDER_TYPE_MARKET
        return types_pb2.ORDER_TYPE_UNSPECIFIED

    def _map_status(self, status):
        s = (status or "").lower()
        if s == "open" or s == "new":
            return types_pb2.ORDER_STATUS_NEW
        if s == "closed" or s == "filled":
            return types_pb2.ORDER_STATUS_FILLED
        if s == "canceled" or s == "cancelled":
            return types_pb2.ORDER_STATUS_CANCELED
        if s == "rejected":
            return types_pb2.ORDER_STATUS_REJECTED
        if s == "expired":
            return types_pb2.ORDER_STATUS_EXPIRED
        if "partial" in s:
            return types_pb2.ORDER_STATUS_PARTIALLY_FILLED
        return types_pb2.ORDER_STATUS_UNSPECIFIED

    def _reverse_map_side(self, side):
        if side == types_pb2.ORDER_SIDE_BUY:
            return "buy"
        if side == types_pb2.ORDER_SIDE_SELL:
            return "sell"
        raise ccxt.BadRequest(f"Invalid or unspecified order side: {side}")

    def _reverse_map_type(self, order_type):
        if order_type == types_pb2.ORDER_TYPE_LIMIT:
            return "limit"
        if order_type == types_pb2.ORDER_TYPE_MARKET:
            return "market"
        raise ccxt.BadRequest(f"Invalid or unspecified order type: {order_type}")

    def _map_order(self, order):
        created_at = Timestamp()
        if "timestamp" in order and order["timestamp"]:
            created_at.FromMilliseconds(order["timestamp"])

        return models_pb2.Order(
            order_id=int(order["id"]) if order["id"].isdigit() else 0,
            client_order_id=order.get("clientOrderId", ""),
            symbol=order["symbol"],
            side=self._map_side(order["side"]),
            type=self._map_type(order["type"]),
            price=self._to_decimal(order.get("price", 0)),
            quantity=self._to_decimal(order.get("amount", 0)),
            executed_qty=self._to_decimal(order.get("filled", 0)),
            avg_price=self._to_decimal(order.get("average", 0)),
            status=self._map_status(order["status"]),
            created_at=created_at,
            update_time=int(order.get("lastTradeTimestamp", 0))
            if order.get("lastTradeTimestamp")
            else 0,
        )

    def _to_decimal(self, value: Union[str, float, int, None]) -> decimal_pb2.Decimal:
        if value is None:
            return decimal_pb2.Decimal(value="0")
        if isinstance(value, float):
            # Use 18 decimal places to avoid precision loss for most crypto assets
            s = "{:.18f}".format(value).rstrip("0").rstrip(".")
            if s == "" or s == "-":
                s = "0"
            return decimal_pb2.Decimal(value=s)
        return decimal_pb2.Decimal(value=str(value))
