"""Tests for ChromaDB helpers."""

from pathlib import Path

from codegen.chroma import add_documents, get_client, get_collection, query_documents


def test_chroma_add_and_query(tmp_path: Path) -> None:
    client = get_client(persist_dir=tmp_path / "chroma")
    collection = get_collection(name="test_collection", client=client)
    assert collection.name == "test_collection"

    add_documents(
        documents=["Python is a programming language.", "ChromaDB is a vector database."],
        ids=["doc1", "doc2"],
        metadatas=[{"topic": "python"}, {"topic": "database"}],
        collection_name="test_collection",
        client=client,
    )

    results = query_documents(
        query_texts=["vector database"],
        n_results=1,
        collection_name="test_collection",
        client=client,
    )

    assert results["ids"][0] == ["doc2"]
    assert "ChromaDB" in results["documents"][0][0]
