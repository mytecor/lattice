"""Opt-in SDK acceptance against a deployed gateway; client key is read from stdin."""
import argparse
import io
import json
import sys
from importlib.metadata import version
from openai import OpenAI
from google import genai
from google.genai import types
from PIL import Image

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--base-url", required=True, help="Gateway root URL, without /v1")
args = parser.parse_args()
if sys.stdin.isatty():
    parser.error("pipe a client API key to stdin; credentials are never printed")
key = sys.stdin.read().strip()
if not key:
    parser.error("empty client API key")
base = args.base_url.rstrip("/")
text = 'A red bicycle stands beside a tree.'
client = OpenAI(base_url=base+'/v1', api_key=key, timeout=300, max_retries=0)
result = client.embeddings.create(model='embedding', input=[text,'A blue boat.'], dimensions=128)
assert [v.index for v in result.data] == [0,1]
assert all(len(v.embedding)==128 and abs(sum(x*x for x in v.embedding)-1)<0.002 for v in result.data)
google = genai.Client(vertexai=False, api_key=key, http_options=types.HttpOptions(base_url=base, api_version='v1beta', timeout=300000))
reference = google.models.embed_content(model='embedding', contents=types.Content(parts=[types.Part(text=text)]), config=types.EmbedContentConfig(output_dimensionality=128))
assert len(reference.embeddings)==1
assert max(abs(a-b) for a,b in zip(result.data[0].embedding, reference.embeddings[0].values))<1e-5
image=io.BytesIO(); Image.new('RGB',(64,64),'red').save(image,format='PNG')
multimodal = google.models.embed_content(model='embedding', contents=types.Content(parts=[types.Part(text='A red square: '),types.Part.from_bytes(data=image.getvalue(),mime_type='image/png')]), config=types.EmbedContentConfig(output_dimensionality=128))
assert len(multimodal.embeddings)==1 and len(multimodal.embeddings[0].values)==128
native = client.embeddings.create(model='embedding',input=[2,2059,1],dimensions=128)
assert len(native.data[0].embedding)==128
model=next(m for m in client.models.list().data if m.id=='embedding')
assert {'text','image','audio','video'} <= set(model.input_modalities)
print(json.dumps({'OpenAI_SDK':'ok','Google_GenAI_SDK':'ok','text_vectors_equivalent':True,'multimodal_image':'ok','native_token_ids':'ok','modalities':model.input_modalities,'dimensions':128,'openai_version':version('openai'),'google_genai_version':version('google-genai')}))
