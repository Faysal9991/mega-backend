"""Validate live/mobile/admin/webhook contracts and executable boundary fixtures."""
import json
from pathlib import Path
from jsonschema import Draft202012Validator, FormatChecker, validators, ValidationError
from openapi_spec_validator import validate
from referencing import Registry, Resource
from referencing.jsonschema import DRAFT202012

root=Path(__file__).resolve().parent
registry=Registry()
for name in ('common.openapi.json','live.openapi.json'):
    path=root/name
    doc=json.loads(path.read_text())
    validate(doc,base_uri=path.as_uri())
    registry=registry.with_resource(path.as_uri(),Resource.from_contents(doc,default_specification=DRAFT202012))
spec=json.loads((root/'live.openapi.json').read_text())
uri=(root/'live.openapi.json').as_uri()
def byte_length(validator,bounds,instance,schema):
    if isinstance(instance,str):
        try:length=len(instance.encode('utf-8'))
        except UnicodeEncodeError:
            yield ValidationError('Invalid Unicode');return
        if length<bounds.get('min',0) or length>bounds['max']:yield ValidationError('UTF-8 byte budget')
def integer_token(validator,required,instance,schema):
    if required and (not isinstance(instance,int) or isinstance(instance,bool)):yield ValidationError('Exact JSON integer token required')
Contract=validators.extend(Draft202012Validator,{'x-utf8-byte-length':byte_length,'x-integer-json-token':integer_token})
def check(name):
    return Contract({'$id':uri,'$ref':'#/components/schemas/'+name,'components':spec['components']},registry=registry,format_checker=FormatChecker())
for fixture in json.loads((root/'fixtures/live.json').read_text()):
    v=check(fixture['schema'])
    v.validate(fixture['body'])
    bad=dict(fixture['body'],actor_id='caller-cannot-assert-authority')
    assert not v.is_valid(bad)
for invalid in ({'title':'Room','room_type':'audio','capacity':501,'seats':3},{'title':'Room','room_type':'audio','capacity':10,'seats':33},{'title':'Room','room_type':'other','capacity':10,'seats':1}):
    assert not check('RoomCreate').is_valid(invalid)
assert not check('HostApply').is_valid({'classification':'universal_admin'})
assert not check('MemberRole').is_valid({'role':'host','reason':'escalation'})
assert not check('MemberMute').is_valid({'reason':'missing decision'})
for path,item in spec['paths'].items():
    for method,op in item.items():
        assert op['security']
        for status in ('401','403','409','422','503'):assert status in op['responses']
        if method in ('post','patch','put','delete') and 'webhook' not in path and not path.endswith(('/token','/reconcile')):
            assert any(p['name']=='Idempotency-Key' for p in op['parameters'])
print('LiveKit/agency OpenAPI and strict boundary fixtures: PASS')

assert not check('RoomCreate').is_valid({'title':'Room','room_type':'video','capacity':10,'seats':1})
assert not check('RoomCreate').is_valid({'title':'Room','room_type':'audio','capacity':10.0,'seats':1})
assert not check('AgencyCreate').is_valid({'name':'অ'*43})
assert not check('PolicyPublish').is_valid({'previous_policy_id':None,'capacity':10,'seats':5,'compatibility':{},'reason':'invalid'})
