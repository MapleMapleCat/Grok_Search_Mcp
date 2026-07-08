package mcpserver

// ServerInstructions gives MCP clients a usage guide during initialize, before
// they inspect individual tool schemas or make their first tool call.
const ServerInstructions = `Grok MCP exposes three read-only tools:

- grok_web_search: use for real-time public web search through Grok web_search.
- grok_x_search: use for real-time X post search through Grok x_search.
- grok_list_models: use to fetch Grok model IDs from upstream /v1/models. The server filters upstream results and exposes only model IDs containing the grok keyword while excluding imagine/video models.

Usage:
- query is required for both tools and should contain the search request text.
- model is optional. If omitted, the server uses the GROK_MODEL environment variable.
- model lists are always filtered by the grok keyword and exclude model IDs containing imagine or video before they are returned to clients.
- allowed_domains limits web results to specific domains; excluded_domains filters domains out. Do not provide allowed_domains and excluded_domains together. Each list supports at most 5 domains.
- enable_image_understanding and enable_image_search are only applicable to grok_web_search.
- grok_x_search accepts only query and model; domain filters and image-related fields are not part of its input schema.

Results:
- Successful tool calls return structured JSON containing answer, citations, sources, and usage when upstream usage data is available.
- Failed searches are returned as MCP tool results with isError=true so the session stays connected; they are not surfaced as Go errors.
- If the client supplies a progressToken, each upstream search round is reported through MCP progress notifications.`
