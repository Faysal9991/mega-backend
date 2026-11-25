"""Delivery contract, executable request/response fixtures and boundary checks."""
import json
from pathlib import Path
from jsonschema import Draft202012Validator, FormatChecker, validators, ValidationError
from openapi_spec_validator import validate
from referencing import Registry, Resource
from referencing.jsonschema import DRAFT202012
root=Path(__file__).resolve().parent
registry=Registry()
for name in ('common','live','wallet','delivery'):
    path=root/(name+'.openapi.json');doc=json.loads(path.read_text())
    validate(doc,base_uri=path.as_uri())
    registry=registry.with_resource(path.as_uri(),Resource.from_contents(doc,default_specification=DRAFT202012))
spec=json.loads((root/'delivery.openapi.json').read_text());uri=(root/'delivery.openapi.json').as_uri()
def byte_length(validator,bounds,value,schema):
    if isinstance(value,str):
        try: n=len(value.encode('utf-8'))
        except UnicodeEncodeError: yield ValidationError('invalid Unicode');return
        if n<bounds.get('min',0) or n>bounds['max']:yield ValidationError('UTF-8 byte budget')
def exact_integer(validator,required,value,schema):
    if required and (not isinstance(value,int) or isinstance(value,bool)):yield ValidationError('exact integer token required')
Contract=validators.extend(Draft202012Validator,{'x-utf8-byte-length':byte_length,'x-integer-json-token':exact_integer})
def check(name):return Contract({'$id':uri,'$ref':'#/components/schemas/'+name,'components':spec['components']},registry=registry,format_checker=FormatChecker())
for fixture in json.loads((root/'fixtures/delivery.json').read_text()):
    check(fixture['schema']).validate(fixture['body'])
    assert not check(fixture['schema']).is_valid(dict(fixture['body'],actor_id='untrusted'))
for fixture in json.loads((root/'fixtures/delivery_responses.json').read_text()):
    check(fixture['schema']).validate(fixture['body'])
base={'session_id':'a'*32,'recipient_id':'b'*32,'gift_id':'c'*32,'quantity':1}
for body in (dict(base,diamond_price=1),dict(base,quantity=0),dict(base,quantity=1.0),dict(base,quantity=9223372036854775808)):
    assert not check('SendGiftRequest').is_valid(body)
assert not check('UploadRequest').is_valid({'purpose':'avatar','content_type':'image/svg+xml','size_bytes':1,'sha256':'A'*43+'='})
assert not check('AttachRequest').is_valid({'expected_asset_id':None})
assert not check('Preferences').is_valid({'gift_received':True})
assert not check('AnnouncementRequest').is_valid({'account_ids':['a'*32]*101,'template':'maintenance','reason':'test'})
assert not check('DeviceRequest').is_valid({'platform':'desktop','token':'secret-token-for-test'})
for path,item in spec['paths'].items():
    for method,op in item.items():
        assert op['security']
        for status in ('401','403','409','422','429','503'):assert status in op['responses']
        if path.startswith('/admin/') and method in ('post','put'):
            assert any(p['name']=='Idempotency-Key' for p in op['parameters'])
print('PASS: delivery OpenAPI, strict request/response fixtures, amounts, uploads, privacy and bounded administration')
