import redis
import json
from datetime import datetime
from app.config import settings

r = redis.Redis.from_url(settings.REDIS_URL, decode_responses=False)

def save_paste(paste_id, data, ttl):
    payload = json.dumps(data).encode()

    if ttl > 0:
        r.set(paste_id, payload, ex=ttl)
    else:
        r.set(paste_id, payload)

def get_paste(paste_id):
    data = r.get(paste_id)
    if not data:
        return None
    return json.loads(data)

def delete_paste(paste_id):
    r.delete(paste_id)