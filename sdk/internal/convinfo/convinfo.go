// Package convinfo lets the SDK's own adapters read conversation settings
// that are not part of the public API.
package convinfo

// RewritesToolResults reports whether conv, an *sdk.Conversation, has a tool
// hook that rewrites tool results (hooks.ToolResultRewriter). Package sdk
// sets it; it reports false for any other value, or before sdk is loaded.
var RewritesToolResults = func(conv any) bool { return false }
