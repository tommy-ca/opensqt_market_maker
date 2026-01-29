import asyncio
import ccxt.pro as ccxtpro
import ccxt.async_support as ccxt
from opensqt.market_maker.v1 import exchange_pb2, types_pb2
from opensqt.market_maker.v1 import exchange_pb2_grpc
from opensqt.market_maker.v1 import resources_pb2 as models_pb2
from google.type import decimal_pb2
from google.protobuf.timestamp_pb2 import Timestamp
import datetime
from .errors import handle_ccxt_exception, retry_transient


class BinanceConnector(exchange_pb2_grpc.ExchangeServiceServicer):
    def __init__(self, api_key, secret_key, exchange_type="futures"):
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
        self.exchange = ccxt.binance(params)
        self.exchange_pro = ccxtpro.binance(params)

    async def stop(self):
        await self.exchange.close()
        await self.exchange_pro.close()

    @handle_ccxt_exception
    @retry_transient()
    @retry_transient()
    async def GetName(self, request, context):
        return exchange_pb2.GetNameResponse(name="binance")

    @handle_ccxt_exception
    @retry_transient()
    async def GetType(self, request, context):
        extype = (
            types_pb2.EXCHANGE_TYPE_FUTURES
            if self.exchange_type == "futures"
            else types_pb2.EXCHANGE_TYPE_SPOT
        )
        return exchange_pb2.GetTypeResponse(type=extype)

    @handle_ccxt_exception
    @retry_transient()
    async def GetLatestPrice(self, request, context):
        symbol = request.symbol
        ticker = await self.exchange.fetch_ticker(symbol)
        return exchange_pb2.GetLatestPriceResponse(
            price=decimal_pb2.Decimal(value=str(ticker["last"]))
        )

    @handle_ccxt_exception
    @retry_transient()
    async def GetSymbolInfo(self, request, context):
        symbol = request.symbol
        if not self.exchange.markets:
            await self.exchange.load_markets()

        market = self.exchange.market(symbol)

        return models_pb2.SymbolInfo(
            symbol=symbol,
            price_precision=market["precision"].get("price", 8),
            quantity_precision=market["precision"].get("amount", 8),
            base_asset=market["base"],
            quote_asset=market["quote"],
            min_quantity=decimal_pb2.Decimal(
                value=str(market["limits"]["amount"]["min"] or "0")
            ),
            min_notional=decimal_pb2.Decimal(
                value=str(market["limits"]["cost"]["min"] or "0")
            ),
            tick_size=decimal_pb2.Decimal(
                value=str(market["precision"].get("price", "0"))
            ),
            step_size=decimal_pb2.Decimal(
                value=str(market["precision"].get("amount", "0"))
            ),
        )

    @handle_ccxt_exception
    @retry_transient()
    async def PlaceOrder(self, request, context):
        symbol = request.symbol
        side = self._reverse_map_side(request.side)
        order_type = self._reverse_map_type(request.type)
        amount = float(request.quantity.value)
        price = (
            float(request.price.value)
            if request.price and request.price.value
            else None
        )

        params = {}
        if request.client_order_id:
            params["clientOrderId"] = request.client_order_id
        if request.post_only:
            params["timeInForce"] = "GTX"
        if request.reduce_only:
            params["reduceOnly"] = True
        if request.use_margin:
            params["margin"] = True

        try:
            order = await self.exchange.create_order(
                symbol, order_type, side, amount, price, params
            )
        except ccxt.DuplicateOrderId:
            # If we get duplicate ID, it means the order already exists.
            # We can try to fetch it.
            if request.client_order_id:
                # CCXT fetch_order usually takes (id, symbol)
                # Some exchanges allow fetching by clientOrderId in params
                try:
                    order = await self.exchange.fetch_order(
                        None, symbol, {"clientOrderId": request.client_order_id}
                    )
                except Exception as e:
                    print(f"Failed to fetch existing order after DuplicateOrderId: {e}")
                    raise
            else:
                raise

        return self._map_order(order)

    @handle_ccxt_exception
    @retry_transient()
    async def BatchPlaceOrders(self, request, context):
        if self.exchange.has.get("createOrders"):
            ccxt_orders = []
            for req in request.orders:
                symbol = req.symbol
                side = self._reverse_map_side(req.side)
                order_type = self._reverse_map_type(req.type)
                amount = float(req.quantity.value)
                price = (
                    float(req.price.value) if req.price and req.price.value else None
                )
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
                print(f"Batch createOrders failed, falling back to sequential: {e}")
                # Fallback to parallel execution if batch fails (e.g. not supported or error)

        # Parallel execution fallback
        tasks = []
        for req in request.orders:
            tasks.append(self.PlaceOrder(req, context))

        results = await asyncio.gather(*tasks, return_exceptions=True)

        response_orders = []
        all_success = True

        for res in results:
            if isinstance(res, Exception):
                print(f"Batch order error: {res}")
                all_success = False
            else:
                response_orders.append(res)

        return exchange_pb2.BatchPlaceOrdersResponse(
            orders=response_orders, all_success=all_success
        )

    @handle_ccxt_exception
    @retry_transient()
    async def CancelOrder(self, request, context):
        await self.exchange.cancel_order(str(request.order_id), request.symbol)
        return exchange_pb2.CancelOrderResponse()

    @handle_ccxt_exception
    @retry_transient()
    async def BatchCancelOrders(self, request, context):
        symbol = request.symbol
        order_ids = request.order_ids

        if self.exchange.has.get("cancelOrders"):
            # CCXT cancel_orders usually takes (ids, symbol, params)
            # ids is a list of strings
            ids = [str(oid) for oid in order_ids]
            await self.exchange.cancel_orders(ids, symbol)
        else:
            tasks = [self.exchange.cancel_order(str(oid), symbol) for oid in order_ids]
            await asyncio.gather(*tasks, return_exceptions=True)

        return exchange_pb2.BatchCancelOrdersResponse()

    @handle_ccxt_exception
    @retry_transient()
    async def GetAccount(self, request, context):
        balance = await self.exchange.fetch_balance()
        total_wallet = str(balance.get("total", {}).get("USDT", "0"))
        available = str(balance.get("free", {}).get("USDT", "0"))

        # Fetch positions to populate the account snapshot
        positions = await self.GetPositions(exchange_pb2.GetPositionsRequest(), context)

        return models_pb2.Account(
            total_wallet_balance=decimal_pb2.Decimal(value=total_wallet),
            total_margin_balance=decimal_pb2.Decimal(value=total_wallet),
            available_balance=decimal_pb2.Decimal(value=available),
            positions=positions.positions,
            account_leverage=10,
        )

    @handle_ccxt_exception
    @retry_transient()
    async def GetOrder(self, request, context):
        order = await self.exchange.fetch_order(str(request.order_id), request.symbol)
        return self._map_order(order)

    @handle_ccxt_exception
    @retry_transient()
    async def GetOpenOrders(self, request, context):
        orders = await self.exchange.fetch_open_orders(request.symbol)
        response_orders = [self._map_order(o) for o in orders]
        return exchange_pb2.GetOpenOrdersResponse(orders=response_orders)

    @handle_ccxt_exception
    @retry_transient()
    async def GetPositions(self, request, context):
        positions = await self.exchange.fetch_positions()
        if request.symbol:
            positions = [p for p in positions if p["symbol"] == request.symbol]

        res = []
        for p in positions:
            if float(p.get("contracts", 0)) == 0 and float(p.get("size", 0)) == 0:
                continue

            res.append(
                models_pb2.Position(
                    symbol=p["symbol"],
                    size=decimal_pb2.Decimal(
                        value=str(p.get("contracts") or p.get("size", "0"))
                    ),
                    entry_price=decimal_pb2.Decimal(
                        value=str(p.get("entryPrice", "0"))
                    ),
                    mark_price=decimal_pb2.Decimal(value=str(p.get("markPrice", "0"))),
                    unrealized_pnl=decimal_pb2.Decimal(
                        value=str(p.get("unrealizedPnl", "0"))
                    ),
                    leverage=int(p.get("leverage", 1)),
                    margin_type=p.get("marginMode", "cross"),
                    liquidation_price=decimal_pb2.Decimal(
                        value=str(p.get("liquidationPrice", "0"))
                    ),
                )
            )
        return exchange_pb2.GetPositionsResponse(positions=res)

    @handle_ccxt_exception
    @retry_transient()
    async def GetSymbols(self, request, context):
        if not self.exchange.markets:
            await self.exchange.load_markets()
        return exchange_pb2.GetSymbolsResponse(
            symbols=list(self.exchange.markets.keys())
        )

    @handle_ccxt_exception
    @retry_transient()
    async def GetFundingRate(self, request, context):
        symbol = request.symbol
        rate = await self.exchange.fetch_funding_rate(symbol)
        return models_pb2.FundingRate(
            exchange="binance",
            symbol=symbol,
            rate=decimal_pb2.Decimal(value=str(rate["fundingRate"])),
            next_funding_time=int(rate.get("nextFundingTime", 0)),
            timestamp=int(rate.get("timestamp", 0)),
        )

    @handle_ccxt_exception
    @retry_transient()
    async def GetFundingRates(self, request, context):
        rates = await self.exchange.fetch_funding_rates()
        res = []
        for symbol, rate in rates.items():
            res.append(
                models_pb2.FundingRate(
                    exchange="binance",
                    symbol=symbol,
                    rate=decimal_pb2.Decimal(value=str(rate["fundingRate"])),
                    next_funding_time=int(rate.get("nextFundingTime", 0)),
                    timestamp=int(rate.get("timestamp", 0)),
                )
            )
        return exchange_pb2.GetFundingRatesResponse(rates=res)

    @handle_ccxt_exception
    @retry_transient()
    async def GetTickers(self, request, context):
        tickers = await self.exchange.fetch_tickers()
        res = []
        for symbol, t in tickers.items():
            res.append(
                models_pb2.Ticker(
                    symbol=symbol,
                    price_change=decimal_pb2.Decimal(value=str(t.get("change", "0"))),
                    price_change_percent=decimal_pb2.Decimal(
                        value=str((t.get("percentage", 0) or 0) / 100.0)
                    ),
                    last_price=decimal_pb2.Decimal(value=str(t.get("last", "0"))),
                    volume=decimal_pb2.Decimal(value=str(t.get("baseVolume", "0"))),
                    quote_volume=decimal_pb2.Decimal(
                        value=str(t.get("quoteVolume", "0"))
                    ),
                    timestamp=int(t.get("timestamp", 0)),
                )
            )
        return exchange_pb2.GetTickersResponse(tickers=res)

    async def SubscribePrice(self, request, context):
        symbols = request.symbols
        if not symbols:
            return

        while True:
            try:
                # CCXT Pro watch_tickers is more efficient for multiple symbols
                tickers = await self.exchange_pro.watch_tickers(symbols)
                # watch_tickers returns a dict of symbols to ticker objects
                # We only want to yield the ones that changed or were requested
                for symbol in symbols:
                    if symbol in tickers:
                        ticker = tickers[symbol]
                        price_change = models_pb2.PriceChange(
                            symbol=ticker["symbol"],
                            price=decimal_pb2.Decimal(value=str(ticker["last"])),
                            timestamp=Timestamp(),
                        )
                        price_change.timestamp.FromMilliseconds(ticker["timestamp"])
                        yield price_change
            except asyncio.CancelledError:
                break
            except Exception as e:
                print(f"Error in SubscribePrice: {e}")
                await asyncio.sleep(5)

    async def SubscribeOrders(self, request, context):
        while True:
            try:
                orders = await self.exchange_pro.watch_orders()
                for order in orders:
                    yield self._map_order_update(order)
            except Exception as e:
                print(f"Error in SubscribeOrders: {e}")
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
                            open=decimal_pb2.Decimal(value=str(last[1])),
                            high=decimal_pb2.Decimal(value=str(last[2])),
                            low=decimal_pb2.Decimal(value=str(last[3])),
                            close=decimal_pb2.Decimal(value=str(last[4])),
                            volume=decimal_pb2.Decimal(value=str(last[5])),
                            timestamp=int(last[0]),
                            is_closed=False,  # CCXT watch_ohlcv usually returns real-time updates
                        )
                except Exception as e:
                    print(f"Error watching Klines for {symbol}: {e}")
                    await asyncio.sleep(5)

        # Merge streams
        queue = asyncio.Queue()

        async def producer(gen):
            async for item in gen:
                await queue.put(item)

        generators = [watch_symbol(s) for s in symbols]
        producers = [asyncio.create_task(producer(gen)) for gen in generators]

        try:
            while True:
                item = await queue.get()
                yield item
        except asyncio.CancelledError:
            for p in producers:
                p.cancel()
            raise

    async def SubscribeAccount(self, request, context):
        while True:
            try:
                balance = await self.exchange_pro.watch_balance()
                # Assumes USDT based futures account for simplicity as per existing code
                total_wallet = str(balance.get("total", {}).get("USDT", "0"))
                available = str(balance.get("free", {}).get("USDT", "0"))

                yield models_pb2.Account(
                    total_wallet_balance=decimal_pb2.Decimal(value=total_wallet),
                    total_margin_balance=decimal_pb2.Decimal(value=total_wallet),
                    available_balance=decimal_pb2.Decimal(value=available),
                    positions=[],
                    account_leverage=10,
                )
            except Exception as e:
                print(f"Error in SubscribeAccount: {e}")
                await asyncio.sleep(5)

    async def SubscribePositions(self, request, context):
        filter_symbol = request.symbol
        while True:
            try:
                positions = await self.exchange_pro.watch_positions()
                # watch_positions returns a list of positions
                for pos in positions:
                    if filter_symbol and pos["symbol"] != filter_symbol:
                        continue

                    yield models_pb2.Position(
                        symbol=pos["symbol"],
                        size=decimal_pb2.Decimal(value=str(pos.get("contracts", 0))),
                        entry_price=decimal_pb2.Decimal(
                            value=str(pos.get("entryPrice", 0))
                        ),
                        mark_price=decimal_pb2.Decimal(
                            value=str(pos.get("markPrice", 0))
                        ),
                        unrealized_pnl=decimal_pb2.Decimal(
                            value=str(pos.get("unrealizedPnl", 0))
                        ),
                        leverage=int(pos.get("leverage", 1)),
                        margin_type=pos.get("marginType", "cross"),
                        isolated_margin=decimal_pb2.Decimal(
                            value=str(pos.get("isolatedWallet", 0))
                        ),
                    )
            except Exception as e:
                print(f"Error in SubscribePositions: {e}")
                await asyncio.sleep(5)

    def _map_order_update(self, order):
        return models_pb2.OrderUpdate(
            order_id=int(order["id"]) if order["id"].isdigit() else 0,
            client_order_id=order.get("clientOrderId", ""),
            symbol=order["symbol"],
            side=self._map_side(order["side"]),
            type=self._map_type(order["type"]),
            status=self._map_status(order["status"]),
            price=decimal_pb2.Decimal(value=str(order.get("price", "0"))),
            quantity=decimal_pb2.Decimal(value=str(order.get("amount", "0"))),
            executed_qty=decimal_pb2.Decimal(value=str(order.get("filled", "0"))),
            avg_price=decimal_pb2.Decimal(value=str(order.get("average", "0"))),
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
        return "buy"

    def _reverse_map_type(self, order_type):
        if order_type == types_pb2.ORDER_TYPE_LIMIT:
            return "limit"
        if order_type == types_pb2.ORDER_TYPE_MARKET:
            return "market"
        return "limit"

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
            price=decimal_pb2.Decimal(value=str(order.get("price", "0"))),
            quantity=decimal_pb2.Decimal(value=str(order.get("amount", "0"))),
            executed_qty=decimal_pb2.Decimal(value=str(order.get("filled", "0"))),
            avg_price=decimal_pb2.Decimal(value=str(order.get("average", "0"))),
            status=self._map_status(order["status"]),
            created_at=created_at,
            update_time=int(order.get("lastTradeTimestamp", 0))
            if order.get("lastTradeTimestamp")
            else 0,
        )
