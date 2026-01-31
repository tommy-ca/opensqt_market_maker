from opensqt.market_maker.v1 import resources_pb2


def test_place_order_request_includes_use_margin_field():
    assert "use_margin" in resources_pb2.PlaceOrderRequest.DESCRIPTOR.fields_by_name
