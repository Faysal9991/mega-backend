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
