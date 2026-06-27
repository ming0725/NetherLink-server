# Agent Package Layout

The Agent package is split by stable runtime responsibilities, not by numbered execution steps. This follows the common shape used by mature agent frameworks: an agent profile/runtime layer, model abstraction, tools and MCP integration, memory, knowledge retrieval, prompts, and guardrails.

## Modules

- `profile`: agent profile lifecycle, ownership, validation, persistence orchestration, and default MCP manifest synchronization.
- `model`: unified model gateway and provider registry. Provider-specific adapters, such as OpenAI-compatible Chat Completions, live here.
- `tools`: local tool contracts, tool calls, tool results, and tool registry.
- `mcp`: Model Context Protocol types, registries, clients, and concrete MCP server packages.
- `memory`: long-lived agent memory records and memory write candidates.
- `knowledge`: retrieval-oriented context such as first-party knowledge search queries and results.
- `context`: prompt context assembly types that combine messages, retrieved sources, and memory.
- `prompts`: system prompts and prompt rendering helpers.
- `guardrails`: tool policy, safety policy, and execution permission checks.

## MCP Layout

- `mcp`: shared protocol types and client/registry interfaces.
- `mcp/mcp_knowledge`: first-party knowledge MCP manifest.
- `mcp/mcp_utility`: local calculate/calendar MCP manifest and deterministic adapter-backed implementation.
- `mcp/mcp_websearch`: external web search/read MCP manifest and local adapter-backed implementation.

## Streaming Progress

Agent streaming uses two separate channels:

- `ai.stream.thinking` is machine-readable execution state. It has `userVisible=false`; clients may use its phase/tool fields for a spinner or tool panel, but must not append it to the assistant chat text. `stepId` pairs `running` with terminal `completed` or `failed`.
- `ai.stream.chunk` with `kind=progress` and `transient=true` is user-facing progress narration. It is emitted before the matching tool `running` state. One sentence is sent as small deltas sharing a `segmentId` and `stepId`; `segmentStart` and `segmentEnd` mark its boundaries.
- `ai.stream.chunk` with `kind=answer` and `transient=false` is persistent final assistant answer content. Provider deltas are forwarded directly and bounded to small backend chunks.

User-facing progress is emitted only for concrete observable actions such as a search or source read. Internal planning, no-tool routing, model reasoning text, and generic answer-generation states do not become chat narration.

For AI conversations only, completed progress steps are persisted in `ai_message_traces` and attached to the resulting assistant message:

- `steps` preserve the user-visible work log so the client can collapse and reopen it above the final answer.
- `summary` is a bounded continuity summary of prior search/read work. Only this summary is injected into later turns in the same AI conversation.
- Full progress wording and raw tool output are not replayed into later model requests.
- `ai.stream.done.assistantMessage.trace` is the terminal folding signal; the client attaches and collapses the live work log instead of waiting for a second event.
- This trace behavior is not used by group chat or private chat AI. Those surfaces continue to use their existing chat and Agent profile boundaries.
