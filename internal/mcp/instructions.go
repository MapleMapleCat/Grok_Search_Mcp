package mcpserver

// ServerInstructions gives MCP clients a usage guide during initialize, before
// they inspect individual tool schemas or make their first tool call.
const ServerInstructions = `Use grok_web_search for current public-web research, grok_x_search for current X posts, and grok_list_models to discover valid model IDs. Omit model to use the server default. Tool errors are recoverable MCP results.`
