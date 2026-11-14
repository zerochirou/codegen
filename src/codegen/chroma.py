"""ChromaDB client and collection helpers."""

import json
import os
from pathlib import Path
from typing import Any

import chromadb
from chromadb.api import ClientAPI
from chromadb.api.models.Collection import Collection
from dotenv import load_dotenv

load_dotenv()


def _read_mcp_persist_dir(config_path: Path) -> Path | None:
    """Read CHROMA_PERSIST_DIR from an mcp_config.json file if present."""
    if not config_path.is_file():
        return None
    try:
        data = json.loads(config_path.read_text(encoding="utf-8"))
        raw = data.get("mcpServers", {}).get("codegen-memory", {}).get("env", {}).get("CHROMA_PERSIST_DIR")
        if not raw:
            return None
        path = Path(raw).expanduser()
        if not path.is_absolute():
            path = (config_path.parent.parent / path).resolve()
        return path
    except (OSError, json.JSONDecodeError, TypeError):
        return None


def resolve_persist_dir(is_global: bool = False) -> Path:
    """Resolve the ChromaDB persistence directory for project or global scope."""
    if is_global:
        global_mcp = Path.home() / ".gemini" / "antigravity-cli" / "mcp_config.json"
        return _read_mcp_persist_dir(global_mcp) or (Path.home() / ".codegen" / "chroma_data")

    env_dir = os.getenv("CHROMA_PERSIST_DIR")
    if env_dir:
        return Path(env_dir).expanduser()

    project_mcp = Path.cwd() / ".gemini" / "mcp_config.json"
    return _read_mcp_persist_dir(project_mcp) or (Path.cwd() / "chroma_data")


def get_persist_dir() -> Path:
    """Return the configured ChromaDB persistence directory."""
    return resolve_persist_dir(is_global=False)


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
