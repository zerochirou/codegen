"""ChromaDB client and collection helpers."""

import os
from pathlib import Path
from typing import Any

import chromadb
from chromadb.api import ClientAPI
from chromadb.api.models.Collection import Collection
from dotenv import load_dotenv

load_dotenv()


def get_persist_dir() -> Path:
    """Return the configured ChromaDB persistence directory."""
    return Path(os.getenv("CHROMA_PERSIST_DIR", "./chroma_data"))


def get_default_collection_name() -> str:
    """Return the configured default ChromaDB collection name."""
    return os.getenv("CHROMA_COLLECTION_NAME", "default")


def get_client(persist_dir: str | Path | None = None) -> ClientAPI:
    """Create a persistent ChromaDB client."""
    path = Path(persist_dir) if persist_dir is not None else get_persist_dir()
    path.mkdir(parents=True, exist_ok=True)
    return chromadb.PersistentClient(path=str(path))


def get_collection(
    name: str | None = None,
    client: ClientAPI | None = None,
) -> Collection:
    """Get or create a ChromaDB collection."""
    chroma_client = client or get_client()
    collection_name = name or get_default_collection_name()
    return chroma_client.get_or_create_collection(name=collection_name)


def add_documents(
    documents: list[str],
    ids: list[str],
    metadatas: list[dict[str, Any]] | None = None,
    collection_name: str | None = None,
    client: ClientAPI | None = None,
) -> None:
    """Add or upsert documents into a ChromaDB collection."""
    collection = get_collection(name=collection_name, client=client)
    collection.upsert(documents=documents, ids=ids, metadatas=metadatas)


def query_documents(
    query_texts: list[str],
    n_results: int = 5,
    collection_name: str | None = None,
    client: ClientAPI | None = None,
) -> dict[str, Any]:
    """Query similar documents from a ChromaDB collection."""
    collection = get_collection(name=collection_name, client=client)
    return collection.query(query_texts=query_texts, n_results=n_results)


def list_documents(
    collection_name: str | None = None,
    client: ClientAPI | None = None,
) -> dict[str, Any]:
    """List all documents stored in a ChromaDB collection."""
    collection = get_collection(name=collection_name, client=client)
    return collection.get()
