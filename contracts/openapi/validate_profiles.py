"""Executable Step 9 schemas/fixtures, including UTF-8 byte assertions."""
import copy
import json
from pathlib import Path
from openapi_spec_validator import validate
from jsonschema import Draft202012Validator, ValidationError, validators
from referencing import Registry, Resource
from referencing.jsonschema import DRAFT202012
root=Path(__file__).resolve().parent
registry=Registry()
for filename in ('common.openapi.json','profiles.openapi.json'):
 path=root/filename;spec=json.loads(path.read_text());validate(spec,base_uri=path.as_uri())
 registry=registry.with_resource(path.as_uri(),Resource.from_contents(spec,default_specification=DRAFT202012))
spec=json.loads((root/'profiles.openapi.json').read_text());uri=(root/'profiles.openapi.json').as_uri()
def byte_length(validator,bounds,instance,schema):
 if isinstance(instance,str):
  try:size=len(instance.encode('utf-8'))
  except UnicodeEncodeError:
   yield ValidationError('Invalid Unicode');return
  if not bounds.get('min',0)<=size<=bounds['max']:yield ValidationError('UTF-8 byte budget')
ByteValidator=validators.extend(Draft202012Validator,{'x-utf8-byte-length':byte_length})
def check(name):return ByteValidator({'$id':uri,'$ref':'#/components/schemas/'+name,'components':spec['components']},registry=registry)
for fixture in json.loads((root/'fixtures/profiles.json').read_text()):
 v=check(fixture['schema']);v.validate(fixture['body']);bad=copy.deepcopy(fixture['body']);bad['data']['contact_email']='private';assert not v.is_valid(bad)
for case in json.loads((root/'fixtures/profile_utf8.json').read_text()):
 assert check('PatchProfile').is_valid({case['field']:case['value']})==case['valid'],case
for body in ({},{'bio':None},{'display_name':None},{'roles':['owner']},{'avatar_url':'x'},{'status':'active'},{'password':'secret'}):assert not check('PatchProfile').is_valid(body),body
for body in ({'name':'owner','permissions':['owner'],'reason':'x'},{'name':'x','permissions':['users.private.read'],'reason':'x'},{'name':'x','permissions':['users.read','users.read'],'reason':'x'}):assert not check('RoleRequest').is_valid(body)
assert len(spec['paths'])==11
print('PASS: Step 9 OpenAPI, strict public projections, request/permission injection negatives and shared Unicode byte/rune fixtures')
