import asyncio
import ccxt.pro as ccxtpro
import ccxt.async_support as ccxt
from opensqt.market_maker.v1 import exchange_pb2
from opensqt.market_maker.v1 import exchange_pb2_grpc
from opensqt.market_maker.v1 import models_pb2
from google.type import decimal_pb2
from google.protobuf.timestamp_pb2 import Timestamp
import datetime


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

    async def GetName(self, request, context):
        return exchange_pb2.GetNameResponse(name="binance")

    async def GetType(self, request, context):
        extype = (
            exchange_pb2.EXCHANGE_TYPE_FUTURES
            if self.exchange_type == "futures"
            else exchange_pb2.EXCHANGE_TYPE_SPOT
        )
        return exchange_pb2.GetTypeResponse(type=extype)

    async def GetLatestPrice(self, request, context):
        symbol = request.symbol
        ticker = await self.exchange.fetch_ticker(symbol)
        return exchange_pb2.GetLatestPriceResponse(
            price=decimal_pb2.Decimal(value=str(ticker["last"]))
        )

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

        order = await self.exchange.create_order(
            symbol, order_type, side, amount, price, params
        )

        return self._map_order(order)

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

        if self.exchange.has.get("createOrders"):
            # This is exchange specific, CCXT structure might vary.
            # For Binance, create_orders takes a list of dicts but the signature in CCXT wrapper
            # usually expects the same args as create_order but as a list?
            # Actually CCXT 'createOrders' takes a list of order definitions.
            # However, mapping that correctly can be tricky.
            # Using asyncio.gather on create_order is safer/easier for parity unless strict atomicity is required.
            # But the spec says "Use ccxt.create_orders (if available)".
            # Let's try to use concurrent execution for now as it's more robust across versions unless we are sure of the structure.
            # Re-reading spec: "Execute via create_orders (if available) or parallel create_order."
            pass

        # Using parallel execution for simplicity and robustness
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
                # Add empty/failed order placeholder? The proto response expects `repeated Order orders`.
                # We should probably return what we can.
            else:
                response_orders.append(res)

        return exchange_pb2.BatchPlaceOrdersResponse(
            orders=response_orders, all_success=all_success
        )

    async def CancelOrder(self, request, context):
        await self.exchange.cancel_order(str(request.order_id), request.symbol)
        return exchange_pb2.CancelOrderResponse()

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

    async def GetAccount(self, request, context):
        balance = await self.exchange.fetch_balance()
        total_wallet = str(balance.get("total", {}).get("USDT", "0"))
        available = str(balance.get("free", {}).get("USDT", "0"))

        return models_pb2.Account(
            total_wallet_balance=decimal_pb2.Decimal(value=total_wallet),
            total_margin_balance=decimal_pb2.Decimal(value=total_wallet),
            available_balance=decimal_pb2.Decimal(value=available),
            positions=[],
            account_leverage=10,
        )

    async def SubscribePrice(self, request, context):
        symbols = request.symbols

        async def watch_ticker_loop(symbol):
            while True:
                try:
                    ticker = await self.exchange_pro.watch_ticker(symbol)
                    price_change = models_pb2.PriceChange(
                        symbol=ticker["symbol"],
                        price=decimal_pb2.Decimal(value=str(ticker["last"])),
                        timestamp=Timestamp(),
                    )
                    price_change.timestamp.FromMilliseconds(ticker["timestamp"])
                    yield price_change
                except Exception as e:
                    print(f"Error in SubscribePrice for {symbol}: {e}")
                    await asyncio.sleep(5)

        # Create generators for each symbol
        generators = [watch_ticker_loop(s) for s in symbols]

        # Merge generators using asyncio.Queue or similar pattern
        # Since we are in an async generator, we can't easily use asyncio.gather on generators directly to yield.
        # Common pattern: spawn tasks that feed a shared queue, and yield from queue.

        queue = asyncio.Queue()

        async def producer(gen):
            async for item in gen:
                await queue.put(item)

        producers = [asyncio.create_task(producer(gen)) for gen in generators]

        try:
            while True:
                # Get next item from any producer
                item = await queue.get()
                yield item
        except asyncio.CancelledError:
            for p in producers:
                p.cancel()
            raise

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
            return models_pb2.ORDER_SIDE_BUY
        if s == "sell":
            return models_pb2.ORDER_SIDE_SELL
        return models_pb2.ORDER_SIDE_UNSPECIFIED

    def _map_type(self, order_type):
        t = (order_type or "").lower()
        if t == "limit":
            return models_pb2.ORDER_TYPE_LIMIT
        if t == "market":
            return models_pb2.ORDER_TYPE_MARKET
        return models_pb2.ORDER_TYPE_UNSPECIFIED

    def _map_status(self, status):
        s = (status or "").lower()
        if s == "open" or s == "new":
            return models_pb2.ORDER_STATUS_NEW
        if s == "closed" or s == "filled":
            return models_pb2.ORDER_STATUS_FILLED
        if s == "canceled" or s == "cancelled":
            return models_pb2.ORDER_STATUS_CANCELED
        if s == "rejected":
            return models_pb2.ORDER_STATUS_REJECTED
        if s == "expired":
            return models_pb2.ORDER_STATUS_EXPIRED
        if "partial" in s:
            return models_pb2.ORDER_STATUS_PARTIALLY_FILLED
        return models_pb2.ORDER_STATUS_UNSPECIFIED

    def _reverse_map_side(self, side):
        if side == models_pb2.ORDER_SIDE_BUY:
            return "buy"
        if side == models_pb2.ORDER_SIDE_SELL:
            return "sell"
        return "buy"

    def _reverse_map_type(self, order_type):
        if order_type == models_pb2.ORDER_TYPE_LIMIT:
            return "limit"
        if order_type == models_pb2.ORDER_TYPE_MARKET:
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
