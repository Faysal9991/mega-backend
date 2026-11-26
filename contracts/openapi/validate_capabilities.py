"""Executable dynamic capability/package contracts and privilege boundaries."""
import copy,json
from pathlib import Path
from openapi_spec_validator import validate
from jsonschema import Draft202012Validator,FormatChecker,ValidationError,validators
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
root=Path(__file__).resolve().parent
registry=Registry()
for name in ('common','profiles','live','capabilities'):
 path=root/(name+'.openapi.json');doc=json.loads(path.read_text());validate(doc,base_uri=path.as_uri());registry=registry.with_resource(path.as_uri(),Resource.from_contents(doc,default_specification=DRAFT202012))
spec=json.loads((root/'capabilities.openapi.json').read_text());uri=(root/'capabilities.openapi.json').as_uri()
def byte_length(validator,bounds,value,schema):
 if isinstance(value,str) and not bounds.get('min',0)<=len(value.encode('utf-8'))<=bounds['max']:yield ValidationError('UTF-8 byte budget')
def integer_token(validator,required,value,schema):
 if required and (not isinstance(value,int) or isinstance(value,bool)):yield ValidationError('exact integer JSON token required')
Contract=validators.extend(Draft202012Validator,{'x-utf8-byte-length':byte_length,'x-integer-json-token':integer_token})
def check(name):return Contract({'$id':uri,'$ref':'#/components/schemas/'+name,'components':spec['components']},registry=registry,format_checker=FormatChecker())
fixtures=json.loads((root/'fixtures/capabilities.json').read_text())
for fixture in fixtures:check(fixture['schema']).validate(fixture['body'])
base=fixtures[0]['body']
for action in ('wallet.adjust','users.restrict','live.participant.global_ban','room.participant.kick','room.join.hidden','room.enter.private'):
 bad=copy.deepcopy(base);bad['definition']['powers']=[action];assert not check('SavePackage').is_valid(bad),action
for field,value in (('diamond_price',0),('diamond_price',1.0),('name','😀'*128),('duration_seconds',0),('active',None),('live_listing_priority',-1),('actor_id','a'*32),('version_id','a'*32)):
 bad=copy.deepcopy(base);bad['definition'][field]=value;assert not check('SavePackage').is_valid(bad),(field,value)
print('capability/package contracts and fixtures PASS')
