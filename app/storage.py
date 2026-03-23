import json
from app.config import settings

# Lazy imports (RAM friendly)
redis = None
psycopg2 = None
sqlite3 = None

class BaseStorage:
    def save(self, key, data, ttl): ...
    def get(self, key): ...
    def delete(self, key): ...
    def get_and_delete(self, key): ...

# REDIS
class RedisStorage(BaseStorage):
    def __init__(self):
        global redis
        import redis
        self.r = redis.Redis.from_url(settings.REDIS_URL, decode_responses=False)

    def save(self, key, data, ttl):
        payload = json.dumps(data).encode()
        if ttl > 0:
            self.r.set(key, payload, ex=ttl)
        else:
            self.r.set(key, payload)

    def get(self, key):
        data = self.r.get(key)
        return json.loads(data) if data else None

    def delete(self, key):
        self.r.delete(key)

    def get_and_delete(self, key):
        data = self.r.execute_command("GETDEL", key)
        return json.loads(data) if data else None

# POSTGRES (OPTIONAL)
class PostgresStorage(BaseStorage):
    def __init__(self):
        global psycopg2
        import psycopg2
        self.conn = psycopg2.connect(settings.POSTGRES_URL)
        self._init_table()

    def _init_table(self):
        with self.conn.cursor() as cur:
            cur.execute("""
                CREATE TABLE IF NOT EXISTS pastes (
                    id TEXT PRIMARY KEY,
                    data JSONB,
                    expire_at TIMESTAMP NULL
                )
            """)
            self.conn.commit()

    def save(self, key, data, ttl):
        with self.conn.cursor() as cur:
            cur.execute("""
                INSERT INTO pastes (id, data, expire_at)
                VALUES (%s, %s, NOW() + (%s || ' seconds')::interval)
                ON CONFLICT (id) DO UPDATE SET
                    data = EXCLUDED.data,
                    expire_at = EXCLUDED.expire_at
            """, (key, json.dumps(data), ttl if ttl > 0 else None))
            self.conn.commit()

    def get(self, key):
        with self.conn.cursor() as cur:
            cur.execute("""
                SELECT data FROM pastes
                WHERE id=%s
                AND (expire_at IS NULL OR expire_at > NOW())
            """, (key,))
            row = cur.fetchone()
            return row[0] if row else None

    def delete(self, key):
        with self.conn.cursor() as cur:
            cur.execute("DELETE FROM pastes WHERE id=%s", (key,))
            self.conn.commit()

    def get_and_delete(self, key):
        data = self.get(key)
        if data:
            self.delete(key)
        return data


# SQLITE (DEFAULT)
class SQLiteStorage(BaseStorage):
    def __init__(self):
        global sqlite3
        import sqlite3
        import threading
        self.conn = sqlite3.connect(settings.SQLITE_PATH, check_same_thread=False)
        # Enable WAL
        self.conn.execute("PRAGMA journal_mode=WAL;")
        self.lock = threading.Lock()
        self._init_table()

    def _init_table(self):
        cur = self.conn.cursor()
        cur.execute("""
            CREATE TABLE IF NOT EXISTS pastes (
                id TEXT PRIMARY KEY,
                data TEXT,
                expire_at INTEGER
            )
        """)
        self.conn.commit()

    def save(self, key, data, ttl):
        import time
        expire_at = int(time.time()) + ttl if ttl > 0 else None

        with self.lock: 
            cur = self.conn.cursor()
            cur.execute("""
                INSERT OR REPLACE INTO pastes (id, data, expire_at)
                VALUES (?, ?, ?)
            """, (key, json.dumps(data), expire_at))
            self.conn.commit()

    def get(self, key):
        import time

        with self.lock:
            cur = self.conn.cursor()
            cur.execute("SELECT data, expire_at FROM pastes WHERE id=?", (key,))
            row = cur.fetchone()

        if not row:
            return None

        data, expire_at = row

        if expire_at and expire_at < int(time.time()):
            self.delete(key)
            return None

        return json.loads(data)

    def delete(self, key):
        with self.lock:
            cur = self.conn.cursor()
            cur.execute("DELETE FROM pastes WHERE id=?", (key,))
            self.conn.commit()

    def get_and_delete(self, key):
        data = self.get(key)
        if data:
            self.delete(key)
        return data

# BACKEND SELECTOR
def get_storage():
    if getattr(settings, "REDIS_URL", None):
        try:
            return RedisStorage()
        except Exception:
            pass

    if getattr(settings, "POSTGRES_URL", None):
        try:
            return PostgresStorage()
        except Exception:
            pass

    return SQLiteStorage()


storage = get_storage()

# PUBLIC API
def save_paste(paste_id, data, ttl):
    storage.save(paste_id, data, ttl)

def get_paste(paste_id):
    return storage.get(paste_id)

def delete_paste(paste_id):
    storage.delete(paste_id)

def get_and_delete_paste(paste_id):
    return storage.get_and_delete(paste_id)