"""Optional development validation; requires openapi-spec-validator/jsonschema."""
import copy
import json
from pathlib import Path
from openapi_spec_validator import validate
from jsonschema import Draft202012Validator, ValidationError, validators
from referencing import Registry, Resource
from referencing.jsonschema import DRAFT202012

root = Path(__file__).resolve().parent
registry = Registry()
for filename in ("common.openapi.json", "operational.openapi.json", "identity.openapi.json"):
    path = root / filename
    spec = json.loads(path.read_text())
    validate(spec, base_uri=path.as_uri())
    registry = registry.with_resource(path.as_uri(), Resource.from_contents(spec, default_specification=DRAFT202012))
spec = json.loads((root / "identity.openapi.json").read_text())
uri = (root / "identity.openapi.json").as_uri()
def utf8_byte_length(validator, bounds, instance, schema):
    # OpenAPI/JSON Schema string lengths count code points. This explicit
    # extension is an assertion in our contract tool, never an ASCII restriction.
    if not isinstance(instance, str):
        return
    try:
        size = len(instance.encode("utf-8"))
    except UnicodeEncodeError:
        yield ValidationError("String must be encodable as UTF-8")
        return
    if size < bounds.get("min", 0) or size > bounds["max"]:
        yield ValidationError("String is outside its UTF-8 byte budget")

ByteValidator = validators.extend(Draft202012Validator, {"x-utf8-byte-length": utf8_byte_length})

def validator(name):
    return ByteValidator({"$id": uri, "$ref": "#/components/schemas/" + name, "components": spec["components"]}, registry=registry)
fixtures = json.loads((root / "fixtures/identity.json").read_text())
for fixture in fixtures:
    v = validator(fixture["schema"])
    v.validate(fixture["body"])
    invalid = copy.deepcopy(fixture["body"])
    invalid["data"]["credential_hash"] = "must-not-leak"
    assert not v.is_valid(invalid)
    invalid = copy.deepcopy(fixture["body"])
    invalid["success"] = False
    assert not v.is_valid(invalid)
installation = "a" * 32
proof = "b" * 43
validator("PasswordRequest").validate({"installation_id": installation, "password": "synthetic-password", "proof_token": proof})
validator("PasswordRequest").validate({"installation_id": installation, "password": "synthetic-password", "access_token": proof, "current_password": "current", "proof_token": None})
validator("IdentifiersRequest").validate({"username": " Example_1 ", "phone": None, "proof_token": proof})
for body in ({"username": None, "proof_token": proof}, {"username": "", "phone": "", "proof_token": proof}, {"phone": "01712345678", "proof_token": proof}, {"phone": "+8801712345678", "staff": True, "proof_token": proof}):
    assert not validator("IdentifiersRequest").is_valid(body)
for body in ({"installation_id": installation, "password": "synthetic-password"}, {"installation_id": installation, "password": "synthetic-password", "proof_token": proof, "access_token": proof, "current_password": "current"}):
    assert not validator("PasswordRequest").is_valid(body)
assert len(spec["paths"]) == 9
for path, methods in spec["paths"].items():
    for operation in methods.values():
        assert "200" in operation["responses"]
        for status in (401, 403, 409, 422, 429, 503):
            assert str(status) in operation["responses"]
print("PASS: common/operational/identity OpenAPI 3.1, identity fixtures, strict projections, request alternatives and negative cases")

# The same fixture values/expected byte counts are executed by Go's real Hash
# and Verify tests, so Unicode code-point/byte drift cannot hide in either layer.
password_cases = json.loads((root / "fixtures/password_utf8.json").read_text())
for fixture in password_cases:
    value = fixture["value"]
    assert len(value.encode("utf-8")) == fixture["utf8_bytes"]
    setup = {"installation_id": installation, "password": value, "proof_token": proof}
    login = {"installation_id": installation, "identifier": "example", "password": value}
    current = {"installation_id": installation, "password": "synthetic-password", "access_token": proof, "current_password": value}
    assert validator("PasswordRequest").is_valid(setup) == fixture["valid_new_password"], (fixture["name"], "setup byte contract")
    assert validator("PasswordLoginRequest").is_valid(login) == fixture["valid_login_input"], (fixture["name"], "login byte contract")
    assert validator("PasswordRequest").is_valid(current) == fixture["valid_login_input"], (fixture["name"], "current-password byte contract")
print(f"PASS: {len(password_cases)} shared ASCII/Bengali/emoji/mixed byte-boundary cases for setup, login and current-password contracts")
