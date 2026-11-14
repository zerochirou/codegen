"""MCP server providing AI memory tools backed by ChromaDB."""

import os
import uuid
from typing import Any

from dotenv import load_dotenv
from mcp.server.mcpserver import MCPServer

from codegen.chroma import add_documents, get_collection, query_documents

load_dotenv()

mcp = MCPServer(os.getenv("MCP_SERVER_NAME", "codegen-mcp"))


@mcp.tool()
def save_memory(
    content: str,
    memory_id: str | None = None,
    tags: list[str] | None = None,
    node_type: str | None = None,
    links: list[str] | None = None,
    collection_name: str | None = None,
) -> dict[str, Any]:
    """Save or update an AI memory/knowledge node in ChromaDB."""
    doc_id = memory_id or str(uuid.uuid4())
    metadata: dict[str, Any] = {}
    if tags:
        metadata["tags"] = ",".join(tags)
    if node_type:
        metadata["node_type"] = node_type
    if links:
        metadata["links"] = ",".join(links)

    add_documents(
        documents=[content],
        ids=[doc_id],
        metadatas=[metadata] if metadata else None,
        collection_name=collection_name,
    )
    return {"id": doc_id, "status": "saved"}


@mcp.tool()
def search_memory(
    query: str,
    n_results: int = 5,
    collection_name: str | None = None,
) -> list[dict[str, Any]]:
    """Search stored AI memories in ChromaDB by semantic similarity."""
    results = query_documents(
        query_texts=[query],
        n_results=n_results,
        collection_name=collection_name,
    )

    ids = results.get("ids", [[]])[0]
    documents = results.get("documents", [[]])[0]
    metadatas = results.get("metadatas", [[]])[0]
    distances = results.get("distances", [[]])[0]

    memories: list[dict[str, Any]] = []
    for idx, doc_id in enumerate(ids):
        memories.append(
            {
                "id": doc_id,
                "content": documents[idx] if idx < len(documents) else None,
                "metadata": metadatas[idx] if idx < len(metadatas) else None,
                "distance": distances[idx] if idx < len(distances) else None,
            }
        )
    return memories


@mcp.tool()
def delete_memory(
    memory_id: str,
    collection_name: str | None = None,
) -> dict[str, str]:
    """Delete a stored AI memory from ChromaDB by its ID."""
    collection = get_collection(name=collection_name)
    collection.delete(ids=[memory_id])
    return {"id": memory_id, "status": "deleted"}


def main() -> None:
    """Run the MCP server over stdio."""
    mcp.run()


if __name__ == "__main__":
    main()
