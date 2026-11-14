"""TUI for inspecting and searching AI context stored in ChromaDB."""

from collections import defaultdict
from typing import Any, ClassVar

from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Horizontal, Vertical, VerticalScroll
from textual.widgets import (
    Button,
    DataTable,
    Footer,
    Header,
    Input,
    Markdown,
    Static,
    TabbedContent,
    TabPane,
)

from codegen.chroma import get_collection, list_documents, query_documents

CONTEXT_BUDGET_TOKENS = 1_000_000
GRID_COLS = 37
GRID_ROWS = 10
GRID_CELLS = GRID_COLS * GRID_ROWS
CATEGORY_COLORS = ("#58a6ff", "#38bdf8", "#a371f7", "#3fb950", "#d29922", "#f778ba")


def _estimate_tokens(text: str) -> int:
    return max(1, len(text) // 4) if text else 0


def _format_tokens(tokens: int) -> str:
    if tokens >= 1_000_000:
        return f"{tokens / 1_000_000:.1f}M"
    if tokens >= 1_000:
        return f"{tokens / 1_000:.1f}k"
    return str(tokens)


class MemoryTUI(App[None]):
    """Interactive terminal UI for browsing ChromaDB AI memories."""

    TITLE = "Codegen Memory"
    SUB_TITLE = "Antigravity Context Inspector"
    CSS = """
    Screen {
        background: #0d1117;
        color: #e6edf3;
    }

    Header {
        background: #161b22;
        color: #58a6ff;
        border-bottom: solid #30363d;
    }

    Footer {
        background: #161b22;
        color: #8b949e;
        border-top: solid #30363d;
    }

    #top-bar {
        height: auto;
        margin: 1 1 0 1;
        layout: horizontal;
        align: left middle;
    }

    #search {
        width: 1fr;
        background: #161b22;
        color: #e6edf3;
        border: tall #30363d;
    }

    #search:focus {
        border: tall #58a6ff;
    }

    #status-badge {
        width: auto;
        min-width: 18;
        height: 3;
        content-align: center middle;
        background: #161b22;
        border: tall #30363d;
        color: #58a6ff;
        padding: 0 2;
        margin-left: 1;
    }

    #reload-btn {
        width: auto;
        min-width: 12;
        height: 3;
        margin-left: 1;
        background: #161b22;
        color: #e6edf3;
        border: tall #30363d;
    }

    #reload-btn:hover {
        border: tall #58a6ff;
        color: #58a6ff;
    }

    TabbedContent {
        height: 1fr;
        margin: 1 1 0 1;
    }

    Tabs {
        background: #161b22;
        border-bottom: solid #30363d;
    }

    Tab {
        color: #8b949e;
    }

    Tab.-active {
        color: #58a6ff;
        text-style: bold;
    }

    #main-split {
        height: 1fr;
        margin-top: 1;
    }

    #list-pane {
        width: 48%;
        min-width: 38;
        height: 1fr;
        border: round #30363d;
        border-title-color: #58a6ff;
        background: #161b22;
        margin-right: 1;
    }

    #memories {
        height: 1fr;
        background: transparent;
    }

    DataTable > .datatable--header {
        background: #1f242d;
        color: #58a6ff;
        text-style: bold;
    }

    DataTable > .datatable--cursor {
        background: #1f6feb 35%;
        color: #e6edf3;
    }

    #detail-pane {
        width: 1fr;
        height: 1fr;
        border: round #30363d;
        border-title-color: #a371f7;
        background: #161b22;
        padding: 0 1;
    }

    #detail-meta {
        height: auto;
        padding: 1 1 0 1;
        color: #8b949e;
        border-bottom: solid #30363d;
        margin-bottom: 1;
    }

    #detail-scroll {
        height: 1fr;
        padding: 0 1 1 1;
    }

    #detail-content {
        margin: 0;
        color: #e6edf3;
    }

    #context-scroll {
        height: 1fr;
        margin-top: 1;
    }

    #context-panel {
        height: auto;
        background: #161b22;
        border: round #30363d;
        padding: 1 2;
    }

    #context-grid-view {
        width: auto;
        margin-right: 3;
    }

    #context-legend {
        width: 1fr;
    }
    """

    BINDINGS: ClassVar[list[Binding | tuple[str, str] | tuple[str, str, str]]] = [
        Binding("q", "quit", "Quit"),
        Binding("/", "focus_search", "Search"),
        Binding("escape", "focus_table", "List", show=False),
        Binding("r", "refresh", "Reload"),
        Binding("c", "copy_selected", "Copy"),
        Binding("d", "delete_selected", "Delete"),
    ]

    def __init__(self, collection_name: str | None = None) -> None:
        super().__init__()
        self.collection_name = collection_name
        self._records: dict[str, dict[str, Any]] = {}
        self._selected_id: str | None = None

    def compose(self) -> ComposeResult:
        yield Header(show_clock=True)
        with Horizontal(id="top-bar"):
            yield Input(
                placeholder="Search context semantically by meaning, keyword, or #tag...",
                id="search",
            )
            yield Static("0 memories", id="status-badge")
            yield Button("↻ Reload", id="reload-btn")
        with TabbedContent(initial="tab-inspector"):
            with TabPane("Inspector", id="tab-inspector"):
                with Horizontal(id="main-split"):
                    with Vertical(id="list-pane") as list_pane:
                        list_pane.border_title = "Memories"
                        yield DataTable(id="memories", cursor_type="row", zebra_stripes=True)
                    with Vertical(id="detail-pane") as detail_pane:
                        detail_pane.border_title = "Context Inspector"
                        yield Static("Select a memory to inspect.", id="detail-meta")
                        with VerticalScroll(id="detail-scroll"):
                            yield Markdown("", id="detail-content")
            with TabPane("Session Context", id="tab-context"):
                with VerticalScroll(id="context-scroll"):
                    with Horizontal(id="context-panel"):
                        yield Static("", id="context-grid-view")
                        yield Static("", id="context-legend")
        yield Footer()

    def on_mount(self) -> None:
        table = self.query_one("#memories", DataTable)
        table.add_columns("ID", "Match", "Tags", "Preview")
        self.load_memories()
        table.focus()

    def load_memories(self, query: str = "") -> None:
        table = self.query_one("#memories", DataTable)
        table.clear()
        self._records.clear()
        self._selected_id = None

        cleaned_query = query.strip()
        distances: list[float] = []
        if cleaned_query:
            raw = query_documents(
                query_texts=[cleaned_query],
                n_results=30,
                collection_name=self.collection_name,
            )
            ids = raw.get("ids", [[]])[0]
            docs = raw.get("documents", [[]])[0]
            metas = raw.get("metadatas", [[]])[0]
            distances = raw.get("distances", [[]])[0] if raw.get("distances") else []
        else:
            raw = list_documents(collection_name=self.collection_name)
            ids = raw.get("ids", [])
            docs = raw.get("documents", [])
            metas = raw.get("metadatas", [])

        for idx, doc_id in enumerate(ids):
            content = docs[idx] if idx < len(docs) and docs[idx] is not None else ""
            meta = metas[idx] if idx < len(metas) and metas[idx] is not None else {}
            tags = str(meta.get("tags", ""))
            short_id = doc_id[:10] + ("…" if len(doc_id) > 10 else "")
            preview = content.replace("\n", " ")[:48]
            match_str = "—"
            if cleaned_query and idx < len(distances):
                score = max(0, min(100, round((1.0 - float(distances[idx])) * 100)))
                match_str = f"{score}%"

            self._records[doc_id] = {
                "content": content,
                "metadata": meta,
                "match": match_str,
            }
            table.add_row(short_id, match_str, tags, preview, key=doc_id)

        badge = self.query_one("#status-badge", Static)
        count = len(ids)
        noun = "memory" if count == 1 else "memories"
        badge.update(f"{count} {noun}" if not cleaned_query else f"{count} matches")

        self._render_context_map()

        if not ids:
            self._show_empty()
        else:
            self._show_detail(ids[0])

    def _render_context_map(self) -> None:
        category_tokens: dict[str, int] = defaultdict(int)
        total_tokens = 0

        for record in self._records.values():
            tokens = _estimate_tokens(record["content"])
            total_tokens += tokens
            tags_raw = str(record["metadata"].get("tags", "")).strip()
            category = tags_raw.split(",")[0].strip() if tags_raw else "untagged"
            category_tokens[category] += tokens

        sorted_cats = sorted(category_tokens.items(), key=lambda x: x[1], reverse=True)
        used_pct = (total_tokens / CONTEXT_BUDGET_TOKENS) * 100
        free_tokens = max(0, CONTEXT_BUDGET_TOKENS - total_tokens)
        free_pct = max(0.0, 100.0 - used_pct)

        # Build cell list for 10x37 grid
        cells: list[str] = []
        for idx, (cat, tok) in enumerate(sorted_cats):
            color = CATEGORY_COLORS[idx % len(CATEGORY_COLORS)]
            cell_count = max(1, round((tok / CONTEXT_BUDGET_TOKENS) * GRID_CELLS))
            cells.extend([f"[{color}]◉[/]"] * cell_count)
            if len(cells) >= GRID_CELLS:
                break

        cells = cells[:GRID_CELLS]
        while len(cells) < GRID_CELLS:
            cells.append("[#30363d]□[/]")

        grid_lines = [
            " ".join(cells[r * GRID_COLS : (r + 1) * GRID_COLS])
            for r in range(GRID_ROWS)
        ]
        self.query_one("#context-grid-view", Static).update("\n".join(grid_lines))

        legend_lines = [
            f"[b #e6edf3]ChromaDB Context[/] · {_format_tokens(total_tokens)}/{_format_tokens(CONTEXT_BUDGET_TOKENS)} tokens ({used_pct:.1f}%)",
            "[b #8b949e]Token usage by category[/]",
        ]
        for idx, (cat, tok) in enumerate(sorted_cats):
            color = CATEGORY_COLORS[idx % len(CATEGORY_COLORS)]
            pct = (tok / CONTEXT_BUDGET_TOKENS) * 100
            legend_lines.append(
                f"[{color}]◉[/] {cat}: {_format_tokens(tok)} tokens ({pct:.1f}%)"
            )
        legend_lines.append(
            f"[#30363d]□[/] Free space: {_format_tokens(free_tokens)} ({free_pct:.1f}%)"
        )
        self.query_one("#context-legend", Static).update("\n".join(legend_lines))

    def _show_empty(self) -> None:
        self._selected_id = None
        self.query_one("#detail-meta", Static).update("No memories found.")
        self.query_one("#detail-content", Markdown).update("")

    def _show_detail(self, doc_id: str) -> None:
        record = self._records.get(doc_id)
        meta_widget = self.query_one("#detail-meta", Static)
        content_widget = self.query_one("#detail-content", Markdown)
        if not record:
            self._show_empty()
            return

        self._selected_id = doc_id
        meta = record["metadata"]
        tags_raw = str(meta.get("tags", "")).strip()
        tags_display = (
            " ".join(f"[bold #38bdf8]#{t.strip()}[/]" for t in tags_raw.split(",") if t.strip())
            if tags_raw
            else "[dim]none[/]"
        )
        match_info = f"  [b]Match:[/b] {record['match']}" if record["match"] != "—" else ""
        extra_meta = {k: v for k, v in meta.items() if k != "tags"}
        extra_line = f"\n[b]Meta:[/b] {extra_meta}" if extra_meta else ""
        meta_widget.update(
            f"[b]ID:[/b] {doc_id}{match_info}\n[b]Tags:[/b] {tags_display}{extra_line}"
        )
        content_widget.update(record["content"])

    def on_input_changed(self, event: Input.Changed) -> None:
        if event.input.id == "search" and not event.value.strip():
            self.load_memories("")

    def on_input_submitted(self, event: Input.Submitted) -> None:
        self.load_memories(event.value)
        self.query_one("#memories", DataTable).focus()

    def on_data_table_row_highlighted(self, event: DataTable.RowHighlighted) -> None:
        if event.row_key and event.row_key.value is not None:
            self._show_detail(str(event.row_key.value))

    def action_focus_search(self) -> None:
        self.query_one("#search", Input).focus()

    def action_focus_table(self) -> None:
        self.query_one("#memories", DataTable).focus()

    def on_button_pressed(self, event: Button.Pressed) -> None:
        if event.button.id == "reload-btn":
            self.action_refresh()

    def action_refresh(self) -> None:
        search_input = self.query_one("#search", Input)
        self.load_memories(search_input.value)
        self.notify("Memories reloaded.", title="Reload")

    def action_copy_selected(self) -> None:
        if not self._selected_id or self._selected_id not in self._records:
            return
        content = self._records[self._selected_id]["content"]
        self.copy_to_clipboard(content)
        self.notify("Copied memory content to clipboard.", title="Copied")

    def action_delete_selected(self) -> None:
        table = self.query_one("#memories", DataTable)
        if table.cursor_row is None or not table.rows:
            return
        row_key = list(table.rows.keys())[table.cursor_row]
        doc_id = str(row_key.value)
        collection = get_collection(name=self.collection_name)
        collection.delete(ids=[doc_id])
        self.notify(f"Deleted {doc_id[:12]}", title="Deleted", severity="warning")
        self.action_refresh()


def main() -> None:
    """Run the ChromaDB Memory Inspector TUI."""
    MemoryTUI().run()


if __name__ == "__main__":
    main()
