# Common foundation contracts

`common.openapi.json` is an OpenAPI 3.1 document with empty paths. It supplies shared schemas/headers/responses only. `fixtures/envelopes.json` contains synthetic contracts compared to actual response writer output by Go tests. No production identities, amounts or business routes are specified.

Run `go test -count=1 ./internal/platform/response` for fixture/implementation drift. For full schema validation, use optional isolated development tooling (no backend runtime dependency):

```sh
python3 -m venv /tmp/biho-step4-openapi-validation
/tmp/biho-step4-openapi-validation/bin/python -m pip install 'openapi-spec-validator==0.7.2' 'jsonschema==4.26.0'
/tmp/biho-step4-openapi-validation/bin/python - <<'PY'
import copy
import json
from pathlib import Path
from openapi_spec_validator import validate
from jsonschema import Draft202012Validator

root = Path('contracts/openapi')
spec = json.loads((root / 'common.openapi.json').read_text())
validate(spec)
assert spec['paths'] == {}
fixtures = json.loads((root / 'fixtures/envelopes.json').read_text())
validators = {
    name: Draft202012Validator({
        '$ref': '#/components/schemas/' + name,
        'components': spec['components'],
    })
    for name in ('SuccessEnvelope', 'ErrorEnvelope', 'Pagination')
}
for fixture in fixtures:
    validators[fixture['schema']].validate(fixture['body'])
negative = []
body = copy.deepcopy(fixtures[0]['body'])
body['success'] = False
negative.append(('SuccessEnvelope', body))
body = copy.deepcopy(fixtures[0]['body'])
body['code'] = 'NOT_FOUND'
negative.append(('SuccessEnvelope', body))
body = copy.deepcopy(fixtures[0]['body'])
body['unexpected'] = 'rejected'
negative.append(('SuccessEnvelope', body))
body = copy.deepcopy(fixtures[-1]['body'])
body['code'] = 'UNKNOWN'
negative.append(('ErrorEnvelope', body))
body = copy.deepcopy(fixtures[-1]['body'])
body['message'] = 'private cause'
negative.append(('ErrorEnvelope', body))
negative += [
    ('Pagination', {'limit': 1, 'has_more': True, 'next_cursor': None}),
    ('Pagination', {'limit': 1, 'has_more': False, 'next_cursor': 'next'}),
    ('Pagination', {'limit': 0, 'has_more': False, 'next_cursor': None}),
]
for schema, body in negative:
    assert not validators[schema].is_valid(body), (schema, body)
print(f'PASS: OpenAPI 3.1, {len(fixtures)} fixtures, {len(negative)} negative cases')
PY
```

Schema lengths count Unicode characters; runtime byte budgets and exact Unicode control/space policy are documented in API_RESPONSE_ENGINE.md. Operation-specific data schemas, security and success codes must be reviewed when endpoints are authorized. No OpenAPI generator, full endpoint request/response validator or client compatibility claim is implied.

## Step 5 operational probes

`operational.openapi.json` adds only GET/HEAD /health/live and /health/ready and references the unchanged common schemas. `fixtures/health.json` is compared to actual router output by `go test -count=1 ./internal/platform/httpserver -run TestHealth`. After installing the optional isolated validation tools above, run from the repository root:

```sh
/tmp/biho-step4-openapi-validation/bin/python - <<'PY'
import json
from pathlib import Path
from openapi_spec_validator import validate
from jsonschema import Draft202012Validator
from referencing import Registry, Resource
from referencing.jsonschema import DRAFT202012

root = Path('contracts/openapi').resolve()
common = json.loads((root / 'common.openapi.json').read_text())
spec = json.loads((root / 'operational.openapi.json').read_text())
validate(common)
uri = (root / 'operational.openapi.json').as_uri()
validate(spec, base_uri=uri)
assert common['paths'] == {}
assert set(spec['paths']) == {'/health/live', '/health/ready'}
registry = Registry().with_resource(
    (root / 'common.openapi.json').as_uri(),
    Resource.from_contents(common, default_specification=DRAFT202012),
)
fixtures = json.loads((root / 'fixtures/health.json').read_text())
for fixture in fixtures:
    path = spec['paths'][fixture['path']]
    assert set(path) == {'get', 'head'}
    schema = path['get']['responses'][str(fixture['status'])]['content']['application/json']['schema']
    assert schema['$ref'].endswith('/' + fixture['schema'])
    validator = Draft202012Validator({
        '$id': uri, 'allOf': [schema], 'components': spec['components'],
    }, registry=registry)
    validator.validate(fixture['body'])
    assert not validator.is_valid({**fixture['body'], 'private_diagnostic': 'rejected'})
    assert 'content' not in path['head']['responses'][str(fixture['status'])]
print('PASS: common/operational OpenAPI 3.1, 6 health fixtures, 6 negative cases')
PY
```

These schema checks do not assert fleet behavior, production availability or dependency health. HEAD behavior and headers are exercised by Go router/startup tests.

## Step 8 identity contracts

`identity.openapi.json` defines only the nine authorized identity/authentication routes. `fixtures/identity.json` includes synthetic current-identity and token envelopes; the current-identity fixture is compared to actual HTTP output by the identity transport test. Request alternatives and forbidden extra projection fields are checked with `python contracts/openapi/validate_identity.py` using the optional isolated validation environment above. Existing safe common error schemas are reused for validation/auth/forbidden/conflict/rate/service failures. UTF-8 byte input limits and nonce/session/revocation semantics are specified in IDENTITY_AUTH.md; no live Google verification follows from schema validation.

Password constraints use `x-utf8-byte-length: {min, max}` on setup, login and current-password inputs. JSON Schema/OpenAPI minLength/maxLength count Unicode code points and cannot assert UTF-8 byte lengths. The executable validator above explicitly implements this extension; generic tooling may ignore it, so clients/generators must implement the documented byte check. Runtime remains authoritative: new passwords are 12–128 UTF-8 bytes without trimming or ASCII restrictions; authentication inputs retain their existing coarse bounds/generic-failure semantics. The 18 shared `fixtures/password_utf8.json` boundary cases run in both the contract validator and Go Hash/Verify tests, including Bengali and emoji that are valid with fewer than 12 code points and invalid above 128 bytes despite fewer than 128 code points.

## Step 9 profiles and scoped administration

profiles.openapi.json defines the bounded profile, own-permission, admin user/restriction and protected-owner role/assignment APIs. Run validate_profiles.py in the same isolated Python validation environment as validate_identity.py. Shared profile_utf8.json assertions run through both Go profile policy and the byte-aware contract validator; profiles.json fixtures exercise strict public response projections. All ordinary errors reuse the common registry/envelopes. See PROFILES_AUTHORIZATION.md for permission, null/empty, concurrency and bootstrap semantics.
