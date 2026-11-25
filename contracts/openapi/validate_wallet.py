"""Validate financial OpenAPI and executable strict boundary/settlement fixtures."""
import copy
import json
from pathlib import Path

from jsonschema import Draft202012Validator, FormatChecker, ValidationError, validators
from openapi_spec_validator import validate
from referencing import Registry, Resource
from referencing.jsonschema import DRAFT202012

root = Path(__file__).resolve().parent
registry = Registry()
for filename in ("common.openapi.json", "wallet.openapi.json"):
    path = root / filename
    document = json.loads(path.read_text())
    validate(document, base_uri=path.as_uri())
    registry = registry.with_resource(path.as_uri(), Resource.from_contents(document, default_specification=DRAFT202012))
spec = json.loads((root / "wallet.openapi.json").read_text())
uri = (root / "wallet.openapi.json").as_uri()


def byte_length(validator, bounds, instance, schema):
    if isinstance(instance, str):
        try:
            length = len(instance.encode("utf-8"))
        except UnicodeEncodeError:
            yield ValidationError("Invalid Unicode")
            return
        if length < bounds.get("min", 0) or length > bounds["max"]:
            yield ValidationError("UTF-8 byte budget")


def integer_token(validator, required, instance, schema):
    # The runtime decoder takes int64 rather than floats or quoted amounts.
    if required and (not isinstance(instance, int) or isinstance(instance, bool)):
        yield ValidationError("An exact JSON integer token is required")


Contract = validators.extend(Draft202012Validator, {
    "x-utf8-byte-length": byte_length,
    "x-integer-json-token": integer_token,
})


def check(name):
    return Contract({"$id": uri, "$ref": "#/components/schemas/" + name, "components": spec["components"]}, registry=registry, format_checker=FormatChecker())


for fixture in json.loads((root / "fixtures/wallet.json").read_text()):
    validator = check(fixture["schema"])
    validator.validate(fixture["body"])
    invalid = copy.deepcopy(fixture["body"])
    invalid["data"]["password"] = "must-not-leak"
    assert not validator.is_valid(invalid)
    if fixture["schema"] == "WITHDRAWAL_REQUESTEDEnvelope":
        invalid = copy.deepcopy(fixture["body"])
        invalid["data"]["settlement_status"] = "settled"
        assert not validator.is_valid(invalid), "Provider-free withdrawal cannot claim settlement"

recipient = "b" * 32
for amount in (1, 9223372036854775807):
    check("TransferRequest").validate({"recipient_id": recipient, "diamonds": amount})
for amount in (None, 0, -1, 1.0, 1.1, "100", True, 9223372036854775808):
    assert not check("TransferRequest").is_valid({"recipient_id": recipient, "diamonds": amount}), amount
for invalid in (
    {"recipient_id": recipient + "\n", "diamonds": 1},
    {"recipient_id": recipient, "diamonds": 1, "actor_id": "a" * 32},
    {"recipient_id": recipient, "diamonds": 1, "fee": 0},
):
    assert not check("TransferRequest").is_valid(invalid)
for invalid in (
    {"host_id": recipient, "gift_id": "rose", "quantity": 1, "price": 1},
    {"host_id": recipient,"gift_id": "rose", "quantity": 0},
    {"host_id": recipient, "gift_id": "rose\n", "quantity": 1},
):
    assert not check("GiftRequest").is_valid(invalid)

# A positive int64 quantity is not arbitrarily capped; the engine checks the
# server catalog price multiplication and resulting wallet debit for overflow.
for quantity in (1, 1001, 9223372036854775807):
    check("GiftRequest").validate({"host_id": recipient, "gift_id": "rose", "quantity": quantity})
for quantity in (0, -1, 1.0, 1.5, 9223372036854775808):
    assert not check("GiftRequest").is_valid({"host_id": recipient, "gift_id": "rose", "quantity": quantity})

transfer_policy = {
    "kind": "transfer", "scope": "all", "enabled": False,
    "effective_at": "2026-01-01T00:00:00Z", "reason": "explicit policy fixture",
    "min_diamonds": 1, "max_diamonds": 100, "daily_limit": 0, "monthly_limit": 0,
    "fee": {"fixed": 0, "bps": 0, "rounding": "ceil"},
}
check("PolicyRequest").validate(transfer_policy)
for field in ("min_diamonds", "max_diamonds", "daily_limit", "monthly_limit", "fee", "enabled"):
    invalid = copy.deepcopy(transfer_policy)
    del invalid[field]
    assert not check("PolicyRequest").is_valid(invalid), field
for field in ("policy_id", "actor_id", "permissions"):
    invalid = copy.deepcopy(transfer_policy)
    invalid[field] = "forged"
    assert not check("PolicyRequest").is_valid(invalid), field
invalid = copy.deepcopy(transfer_policy)
invalid["reason"] += "\n"
assert not check("PolicyRequest").is_valid(invalid)
withdrawal_policy = dict(transfer_policy, kind="withdrawal", review_only=True)
check("PolicyRequest").validate(withdrawal_policy)
withdrawal_policy["review_only"] = False
assert not check("PolicyRequest").is_valid(withdrawal_policy)
gift_policy = {"kind": "gift", "scope": "non_agency", "effective_at": "2026-01-01T00:00:00Z", "enabled": False, "reason": "explicit synthetic policy", "vat": "inclusive_diamonds", "tax_jurisdiction": "synthetic-fixture", "rounding": "largest_remainder", "host_bps": 5000, "agency_bps": 0, "platform_bps": 4000, "tax_bps": 1000, "catalog": {"rose": 10}}
check("PolicyRequest").validate(gift_policy)
for field in ("vat", "tax_jurisdiction", "rounding", "host_bps", "agency_bps", "platform_bps", "tax_bps", "catalog"):
    invalid = copy.deepcopy(gift_policy)
    del invalid[field]
    assert not check("PolicyRequest").is_valid(invalid), field
activity_policy = {"kind": "activity", "scope": "audio_host", "effective_at": "2026-01-01T00:00:00Z", "enabled": False, "reason": "explicit synthetic period", "activity": {"timezone": "Asia/Dhaka", "period": "previous_completed_calendar_month", "minimum_qualifying_days": 15, "minimum_daily_live_minutes": 120}}
check("PolicyRequest").validate(activity_policy)
assert not check("ReviewRequest").is_valid({"action": "settle", "reason": "no provider"})
assert not check("AssociationRequest").is_valid({"agency_id": recipient, "classification": "staff", "effective_from": "2026-01-01T00:00:00Z", "reason": "invalid classification"})
for path, operations in spec["paths"].items():
    for method, operation in operations.items():
        assert "401" in operation["responses"] and "403" in operation["responses"] and "409" in operation["responses"] and "503" in operation["responses"]
        if method in ("post", "put"):
            assert any(p["name"] == "Idempotency-Key" and p["required"] for p in operation["parameters"]), path
print("PASS: financial OpenAPI, shared HTTP fixtures, int64/strict DTO/catalog/policy boundaries and non-settling withdrawal contracts")
