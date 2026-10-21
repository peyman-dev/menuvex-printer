"""Database setup. PostgreSQL in production (DATABASE_URL), SQLite in tests.

The same SQLAlchemy models run on both; JSONB is used on PostgreSQL and
plain JSON elsewhere. Production DDL lives in migrations/001_init.sql and is
the authoritative schema; create_all is only a convenience for local dev.
"""
import os

from sqlalchemy import BigInteger, Integer, JSON, create_engine
from sqlalchemy.dialects.postgresql import JSONB
from sqlalchemy.orm import declarative_base, sessionmaker

DATABASE_URL = os.environ.get(
    "DATABASE_URL", "postgresql+psycopg://menuvex:menuvex@localhost:5432/menuvex_sync"
)

connect_args = {"check_same_thread": False} if DATABASE_URL.startswith("sqlite") else {}
engine = create_engine(DATABASE_URL, connect_args=connect_args, pool_pre_ping=True, future=True)
SessionLocal = sessionmaker(bind=engine, autoflush=False, expire_on_commit=False, future=True)
Base = declarative_base()

JSONType = JSON().with_variant(JSONB(), "postgresql")

# BIGINT on PostgreSQL (BIGSERIAL sequences); SQLite only autoincrements
# INTEGER PRIMARY KEY columns, so test/local runs map to INTEGER there.
BigIntPK = BigInteger().with_variant(Integer(), "sqlite")


def get_db():
    db = SessionLocal()
    try:
        yield db
    finally:
        db.close()
