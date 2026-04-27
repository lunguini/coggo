package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/lunguini/coggo/internal/types"
)

// registerTools attaches all 12 Coggo tools to the underlying MCP server.
//
// The descriptions here ARE the API contract with AI clients — if a model
// can't tell when to call a tool from its description, it won't be called.
// Wording follows spec §7.3.
func (s *Server) registerTools() {
	s.mcpServer.AddTool(mcp.NewTool("coggo_entity_get",
		mcp.WithDescription(
			"Fetch a single Coggo entity by ID from a peer.\n\n"+
				"Use when: you need a specific entity by ID, e.g., to read the current state of a project, decision, or goal.\n\n"+
				"Args:\n"+
				"  peer (string, required): peer name or DID\n"+
				"  id (string, required): entity ID\n"+
				fieldsParamDoc+
				"Returns: a JSON entity object with type, fields, and provenance."),
		mcp.WithString("peer", mcp.Description("Peer name or DID"), mcp.Required()),
		mcp.WithString("id", mcp.Description("Entity ID"), mcp.Required()),
		fieldsParam(),
	), s.handleEntityGet)

	s.mcpServer.AddTool(mcp.NewTool("coggo_entity_query",
		mcp.WithDescription(
			"List entities of a given type matching optional filters.\n\n"+
				"Use when: you need a list of entities matching some criteria, e.g., \"all active projects\" or \"decisions tagged X\".\n\n"+
				"Args:\n"+
				"  peer (string, required): peer name or DID\n"+
				"  type (string, required): entity type to query\n"+
				"  filters (object, optional): field-value equality filters\n"+
				"  limit (number, optional, default 50): max results\n"+
				"  include_archived (boolean, optional, default false)\n"+
				fieldsParamDoc+
				"Returns: a JSON array of entity objects."),
		mcp.WithString("peer", mcp.Description("Peer name or DID"), mcp.Required()),
		mcp.WithString("type", mcp.Description("Entity type"), mcp.Required()),
		mcp.WithObject("filters", mcp.Description("Field-value equality filters"), mcp.AdditionalProperties(true)),
		mcp.WithNumber("limit", mcp.Description("Max results (default 50)")),
		mcp.WithBoolean("include_archived", mcp.Description("Include archived entities (default false)")),
		fieldsParam(),
	), s.handleEntityQuery)

	s.mcpServer.AddTool(mcp.NewTool("coggo_relation_query",
		mcp.WithDescription(
			"Query relations (graph edges) optionally filtered by source, target, or type.\n\n"+
				"Use when: you need to explore the graph — what does this depend on, what affects what, what was superseded by what.\n\n"+
				"Args:\n"+
				"  peer (string, required)\n"+
				"  from (string, optional): source entity ID\n"+
				"  to (string, optional): target entity ID\n"+
				"  type (string, optional): relationship type\n"+
				"  limit (number, optional, default 50)\n"+
				fieldsParamDoc+
				"Returns: a JSON array of relation objects."),
		mcp.WithString("peer", mcp.Description("Peer name or DID"), mcp.Required()),
		mcp.WithString("from", mcp.Description("Source entity ID")),
		mcp.WithString("to", mcp.Description("Target entity ID")),
		mcp.WithString("type", mcp.Description("Relationship type")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 50)")),
		fieldsParam(),
	), s.handleRelationQuery)

	s.mcpServer.AddTool(mcp.NewTool("coggo_semantic_search",
		mcp.WithDescription(
			"Semantic search across entities using a natural-language query.\n\n"+
				"Use when: you don't know the exact entity name or ID, but want to find things related to a concept. "+
				"Falls back to substring search if vector search is unavailable or returns weak matches.\n\n"+
				"Args:\n"+
				"  peer (string, required)\n"+
				"  query (string, required): natural language query\n"+
				"  type_filter (string, optional): restrict to a single entity type\n"+
				"  limit (number, optional, default 10)\n"+
				fieldsParamDoc+
				"Returns: a JSON array of {entity, score} ranked by similarity."),
		mcp.WithString("peer", mcp.Description("Peer name or DID"), mcp.Required()),
		mcp.WithString("query", mcp.Description("Natural-language query"), mcp.Required()),
		mcp.WithString("type_filter", mcp.Description("Optional entity type to restrict to")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 10)")),
		fieldsParam(),
	), s.handleSemanticSearch)

	s.mcpServer.AddTool(mcp.NewTool("coggo_time_travel",
		mcp.WithDescription(
			"Reconstruct entity state as it existed at a historical point in time.\n\n"+
				"Use when: you need historical state — \"what did this look like a month ago\", \"what decisions were active when X happened\". "+
				"Slower than current-state queries; use sparingly.\n\n"+
				"Args:\n"+
				"  peer (string, required)\n"+
				"  query (object, required): same shape as entity_query (type, filters, limit, include_archived)\n"+
				"  as_of (string, required): RFC3339 timestamp\n"+
				fieldsParamDoc+
				"Returns: a JSON array of entity objects as they existed at as_of."),
		mcp.WithString("peer", mcp.Description("Peer name or DID"), mcp.Required()),
		mcp.WithObject("query", mcp.Description("EntityQuery: {type, filters?, limit?, include_archived?}"), mcp.Required(), mcp.AdditionalProperties(true)),
		mcp.WithString("as_of", mcp.Description("RFC3339 timestamp"), mcp.Required()),
		fieldsParam(),
	), s.handleTimeTravel)

	s.mcpServer.AddTool(mcp.NewTool("coggo_type_list",
		mcp.WithDescription(
			"List all entity and relationship types defined in a peer.\n\n"+
				"Use when: starting to work with a peer; call this first before creating entities to know what types exist.\n\n"+
				"Args:\n"+
				"  peer (string, required)\n"+
				fieldsParamDoc+
				"Returns: a JSON object {entity_types: [...], relationship_types: [...]}."),
		mcp.WithString("peer", mcp.Description("Peer name or DID"), mcp.Required()),
		fieldsParam(),
	), s.handleTypeList)

	s.mcpServer.AddTool(mcp.NewTool("coggo_type_describe",
		mcp.WithDescription(
			"Describe a single entity or relationship type, including all its fields.\n\n"+
				"Use when: you need to know what fields a type accepts before creating an entity of that type.\n\n"+
				"Args:\n"+
				"  peer (string, required)\n"+
				"  type_name (string, required)\n"+
				"  is_relationship (boolean, optional, default false)\n"+
				fieldsParamDoc+
				"Returns: the full type definition as JSON."),
		mcp.WithString("peer", mcp.Description("Peer name or DID"), mcp.Required()),
		mcp.WithString("type_name", mcp.Description("Type name to describe"), mcp.Required()),
		mcp.WithBoolean("is_relationship", mcp.Description("True for relationship types (default false)")),
		fieldsParam(),
	), s.handleTypeDescribe)

	s.mcpServer.AddTool(mcp.NewTool("coggo_entity_create",
		mcp.WithDescription(
			"Create a new entity in a peer.\n\n"+
				"Use when: capturing new state — a new project, a decision being made, a goal being set, an observation being logged. "+
				"AI clients should call this PROACTIVELY when the user makes a substantive decision, sets a goal, or notes something worth remembering. "+
				"Do not ask permission first; log and continue.\n\n"+
				"Args:\n"+
				"  peer (string, required)\n"+
				"  type (string, required): must reference a defined type in this peer\n"+
				"  fields (object, required): field values per the type definition\n\n"+
				"Returns: the created entity with its assigned ID.\n\n"+
				"v0.1 (loose validation): missing required fields → error; type mismatches → warning, accepted; unknown fields → accepted and stored."),
		mcp.WithString("peer", mcp.Description("Peer name or DID"), mcp.Required()),
		mcp.WithString("type", mcp.Description("Entity type"), mcp.Required()),
		mcp.WithObject("fields", mcp.Description("Field values"), mcp.Required(), mcp.AdditionalProperties(true)),
	), s.handleEntityCreate)

	s.mcpServer.AddTool(mcp.NewTool("coggo_entity_update",
		mcp.WithDescription(
			"Update fields on an existing entity. Generates an EntityUpdated event; original state remains in the event log for time-travel.\n\n"+
				"Use when: an existing entity's state changes — a project moves from \"active\" to \"paused\", a goal's target date shifts.\n\n"+
				"Args:\n"+
				"  peer (string, required)\n"+
				"  id (string, required)\n"+
				"  fields (object, required): fields to update (merged into existing data)\n\n"+
				"Returns: the updated entity."),
		mcp.WithString("peer", mcp.Description("Peer name or DID"), mcp.Required()),
		mcp.WithString("id", mcp.Description("Entity ID"), mcp.Required()),
		mcp.WithObject("fields", mcp.Description("Fields to update"), mcp.Required(), mcp.AdditionalProperties(true)),
	), s.handleEntityUpdate)

	s.mcpServer.AddTool(mcp.NewTool("coggo_relation_create",
		mcp.WithDescription(
			"Create a relation (graph edge) between two entities.\n\n"+
				"Use when: connecting entities — recording that a project depends on another, that a decision supersedes a previous one.\n\n"+
				"Args:\n"+
				"  peer (string, required)\n"+
				"  from (string, required): source entity ID\n"+
				"  to (string, required): target entity ID\n"+
				"  type (string, required): must reference a defined relationship type\n"+
				"  data (object, optional): edge metadata\n\n"+
				"Returns: the created relation."),
		mcp.WithString("peer", mcp.Description("Peer name or DID"), mcp.Required()),
		mcp.WithString("from", mcp.Description("Source entity ID"), mcp.Required()),
		mcp.WithString("to", mcp.Description("Target entity ID"), mcp.Required()),
		mcp.WithString("type", mcp.Description("Relationship type"), mcp.Required()),
		mcp.WithObject("data", mcp.Description("Optional edge data"), mcp.AdditionalProperties(true)),
	), s.handleRelationCreate)

	s.mcpServer.AddTool(mcp.NewTool("coggo_type_define",
		mcp.WithDescription(
			"Define a new entity or relationship type in a peer. Coggo's type system is open; new types are added via this tool.\n\n"+
				"Use when: the user's life or work introduces a structure that doesn't fit existing types.\n\n"+
				"Args:\n"+
				"  peer (string, required)\n"+
				"  name (string, required): new type name\n"+
				"  description (string, required)\n"+
				"  fields (array, required): array of field definitions {name, type, required, default?, description?}\n"+
				"    Field types: string, number, boolean, timestamp, reference, list_of\n"+
				"  is_relationship (boolean, optional, default false): true to define a relationship type\n\n"+
				"Returns: the created type definition."),
		mcp.WithString("peer", mcp.Description("Peer name or DID"), mcp.Required()),
		mcp.WithString("name", mcp.Description("New type name"), mcp.Required()),
		mcp.WithString("description", mcp.Description("Human-readable description"), mcp.Required()),
		mcp.WithArray("fields", mcp.Description("Array of field definitions"), mcp.Required(), mcp.Items(map[string]any{"type": "object", "additionalProperties": true})),
		mcp.WithBoolean("is_relationship", mcp.Description("True for relationship types (default false)")),
	), s.handleTypeDefine)

	s.mcpServer.AddTool(mcp.NewTool("coggo_cross_peer_query",
		mcp.WithDescription(
			"Run an entity query against multiple peers and aggregate results.\n\n"+
				"Use when: a question spans peers — \"show me open work across personal and business\", \"what decisions are active anywhere\".\n\n"+
				"Args:\n"+
				"  query (object, required): EntityQuery shape {type, filters?, limit?, include_archived?}\n"+
				"  peers (array of strings, required): peer names or DIDs to query\n"+
				"  limit_per_peer (number, optional, default 50)\n"+
				fieldsParamDoc+
				"Returns: a JSON array of {peer, entities} objects, one per peer queried."),
		mcp.WithObject("query", mcp.Description("EntityQuery: {type, filters?, limit?, include_archived?}"), mcp.Required(), mcp.AdditionalProperties(true)),
		mcp.WithArray("peers", mcp.Description("Peer names or DIDs"), mcp.Required(), mcp.Items(map[string]any{"type": "string"})),
		mcp.WithNumber("limit_per_peer", mcp.Description("Max results per peer (default 50)")),
		fieldsParam(),
	), s.handleCrossPeerQuery)
}

// ---- handlers ----

func (s *Server) handleEntityGet(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	peerArg, err := req.RequireString("peer")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	id, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	did, _, _, err := authorizeForPeer(ctx, peerArg)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return s.routeQueryProjected(ctx, did, "entity.get", map[string]any{"id": id}, getFieldsArg(req))
}

func (s *Server) handleEntityQuery(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	peerArg, err := req.RequireString("peer")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	typ, err := req.RequireString("type")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	did, _, _, err := authorizeForPeer(ctx, peerArg)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	q := types.EntityQuery{Type: typ}
	args := req.GetArguments()
	if f, ok := args["filters"].(map[string]any); ok {
		q.Filters = f
	}
	q.Limit = req.GetInt("limit", 0)
	if b, ok := args["include_archived"].(bool); ok {
		q.IncludeArchived = b
	}
	return s.routeQueryProjected(ctx, did, "entity.query", q, getFieldsArg(req))
}

func (s *Server) handleRelationQuery(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	peerArg, err := req.RequireString("peer")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	did, _, _, err := authorizeForPeer(ctx, peerArg)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	args := map[string]any{}
	if v := req.GetString("from", ""); v != "" {
		args["from"] = v
	}
	if v := req.GetString("to", ""); v != "" {
		args["to"] = v
	}
	if v := req.GetString("type", ""); v != "" {
		args["type"] = v
	}
	if v := req.GetInt("limit", 0); v != 0 {
		args["limit"] = v
	}
	return s.routeQueryProjected(ctx, did, "relation.query", args, getFieldsArg(req))
}

func (s *Server) handleSemanticSearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	peerArg, err := req.RequireString("peer")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	query, err := req.RequireString("query")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	did, _, _, err := authorizeForPeer(ctx, peerArg)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	args := map[string]any{"query": query, "limit": req.GetInt("limit", 10)}
	if v := req.GetString("type_filter", ""); v != "" {
		args["type_filter"] = v
	}
	return s.routeQueryProjected(ctx, did, "semantic.search", args, getFieldsArg(req))
}

func (s *Server) handleTimeTravel(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	peerArg, err := req.RequireString("peer")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	asOfStr, err := req.RequireString("as_of")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	asOf, err := time.Parse(time.RFC3339, asOfStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("as_of must be RFC3339: %v", err)), nil
	}
	did, _, _, err := authorizeForPeer(ctx, peerArg)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	args := req.GetArguments()
	queryRaw, ok := args["query"].(map[string]any)
	if !ok {
		return mcp.NewToolResultError("query is required"), nil
	}
	q, err := decodeEntityQuery(queryRaw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return s.routeQueryProjected(ctx, did, "time.travel", map[string]any{"query": q, "as_of": asOf}, getFieldsArg(req))
}

func (s *Server) handleTypeList(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	peerArg, err := req.RequireString("peer")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	did, _, _, err := authorizeForPeer(ctx, peerArg)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return s.routeQueryProjected(ctx, did, "type.list", map[string]any{}, getFieldsArg(req))
}

func (s *Server) handleTypeDescribe(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	peerArg, err := req.RequireString("peer")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	name, err := req.RequireString("type_name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	did, _, _, err := authorizeForPeer(ctx, peerArg)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	args := map[string]any{"name": name}
	if b, ok := req.GetArguments()["is_relationship"].(bool); ok {
		args["is_relationship"] = b
	}
	return s.routeQueryProjected(ctx, did, "type.describe", args, getFieldsArg(req))
}

func (s *Server) handleEntityCreate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	peerArg, err := req.RequireString("peer")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	typ, err := req.RequireString("type")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	did, _, tok, err := authorizeForPeer(ctx, peerArg)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	fields, ok := req.GetArguments()["fields"].(map[string]any)
	if !ok {
		return mcp.NewToolResultError("fields is required"), nil
	}
	return s.routeWrite(ctx, did, "entity.create", map[string]any{
		"type":      typ,
		"fields":    fields,
		"client_id": clientIDFromToken(tok),
	})
}

func (s *Server) handleEntityUpdate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	peerArg, err := req.RequireString("peer")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	id, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	did, _, tok, err := authorizeForPeer(ctx, peerArg)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	fields, ok := req.GetArguments()["fields"].(map[string]any)
	if !ok {
		return mcp.NewToolResultError("fields is required"), nil
	}
	return s.routeWrite(ctx, did, "entity.update", map[string]any{
		"id":        id,
		"fields":    fields,
		"client_id": clientIDFromToken(tok),
	})
}

func (s *Server) handleRelationCreate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	peerArg, err := req.RequireString("peer")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	from, err := req.RequireString("from")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	to, err := req.RequireString("to")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	typ, err := req.RequireString("type")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	did, _, tok, err := authorizeForPeer(ctx, peerArg)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	args := map[string]any{"from": from, "to": to, "type": typ, "client_id": clientIDFromToken(tok)}
	if d, ok := req.GetArguments()["data"].(map[string]any); ok {
		args["data"] = d
	}
	return s.routeWrite(ctx, did, "relation.create", args)
}

func (s *Server) handleTypeDefine(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	peerArg, err := req.RequireString("peer")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	desc, err := req.RequireString("description")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	did, _, tok, err := authorizeForPeer(ctx, peerArg)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	rawFields, ok := req.GetArguments()["fields"].([]any)
	if !ok {
		return mcp.NewToolResultError("fields must be an array"), nil
	}
	fields := make([]types.FieldDef, 0, len(rawFields))
	for _, rf := range rawFields {
		m, ok := rf.(map[string]any)
		if !ok {
			return mcp.NewToolResultError("each field must be an object"), nil
		}
		fd, err := decodeFieldDef(m)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		fields = append(fields, fd)
	}
	args := map[string]any{
		"name":        name,
		"description": desc,
		"fields":      fields,
		"client_id":   clientIDFromToken(tok),
	}
	if b, ok := req.GetArguments()["is_relationship"].(bool); ok {
		args["is_relationship"] = b
	}
	return s.routeWrite(ctx, did, "type.define", args)
}

func (s *Server) handleCrossPeerQuery(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	queryRaw, ok := args["query"].(map[string]any)
	if !ok {
		return mcp.NewToolResultError("query is required"), nil
	}
	q, err := decodeEntityQuery(queryRaw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	peersRaw, ok := args["peers"].([]any)
	if !ok || len(peersRaw) == 0 {
		return mcp.NewToolResultError("peers is required (non-empty array)"), nil
	}
	if v := req.GetInt("limit_per_peer", 0); v != 0 {
		q.Limit = v
	}
	fields := getFieldsArg(req)

	type peerResult struct {
		Peer     string `json:"peer"`
		Entities any    `json:"entities,omitempty"`
		Error    string `json:"error,omitempty"`
	}
	out := make([]peerResult, 0, len(peersRaw))
	for _, pr := range peersRaw {
		name, ok := pr.(string)
		if !ok {
			return mcp.NewToolResultError("peers entries must be strings"), nil
		}
		did, _, _, authErr := authorizeForPeer(ctx, name)
		if authErr != nil {
			out = append(out, peerResult{Peer: name, Error: authErr.Error()})
			continue
		}
		respMsg, routeErr := s.routeFederation(ctx, did, types.FedMsgQuery, "entity.query", q)
		if routeErr != nil {
			out = append(out, peerResult{Peer: name, Error: routeErr.Error()})
			continue
		}
		if respMsg.Type == types.FedMsgError {
			out = append(out, peerResult{Peer: name, Error: string(respMsg.Payload)})
			continue
		}
		payload := []byte(respMsg.Payload)
		if len(fields) > 0 {
			if projected, perr := projectJSON(payload, fields); perr == nil {
				payload = projected
			}
		}
		var entities any
		if err := json.Unmarshal(payload, &entities); err != nil {
			out = append(out, peerResult{Peer: name, Error: err.Error()})
			continue
		}
		out = append(out, peerResult{Peer: name, Entities: entities})
	}
	body, err := json.Marshal(out)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(body)), nil
}

// ---- routing helpers ----

// routeQueryProjected runs a federation query and, when fields is non-empty,
// applies the GraphQL-style projection (with optional `:N` truncation) before
// returning the response as the tool result.
func (s *Server) routeQueryProjected(ctx context.Context, targetDID, op string, args any, fields []string) (*mcp.CallToolResult, error) {
	respMsg, err := s.routeFederation(ctx, targetDID, types.FedMsgQuery, op, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if respMsg.Type == types.FedMsgError {
		return mcp.NewToolResultError(string(respMsg.Payload)), nil
	}
	payload := []byte(respMsg.Payload)
	if len(fields) > 0 {
		projected, perr := projectJSON(payload, fields)
		if perr == nil {
			payload = projected
		}
	}
	return mcp.NewToolResultText(string(payload)), nil
}

// getFieldsArg pulls an optional list-of-strings `fields` parameter from the
// tool request. Returns nil when omitted, empty, or malformed.
func getFieldsArg(req mcp.CallToolRequest) []string {
	raw, ok := req.GetArguments()["fields"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s *Server) routeWrite(ctx context.Context, targetDID, op string, args any) (*mcp.CallToolResult, error) {
	return s.routeAndReturn(ctx, targetDID, types.FedMsgWrite, op, args)
}

func (s *Server) routeAndReturn(ctx context.Context, targetDID string, msgType types.FederationMessageType, op string, args any) (*mcp.CallToolResult, error) {
	respMsg, err := s.routeFederation(ctx, targetDID, msgType, op, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if respMsg.Type == types.FedMsgError {
		return mcp.NewToolResultError(string(respMsg.Payload)), nil
	}
	return mcp.NewToolResultText(string(respMsg.Payload)), nil
}

func (s *Server) routeFederation(ctx context.Context, targetDID string, msgType types.FederationMessageType, op string, args any) (types.FederationMessage, error) {
	router := routerFromCtx(ctx)
	if router == nil {
		router = s.Router
	}
	argsRaw, err := json.Marshal(args)
	if err != nil {
		return types.FederationMessage{}, fmt.Errorf("marshal args: %w", err)
	}
	envelope := map[string]any{"op": op, "args": json.RawMessage(argsRaw)}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return types.FederationMessage{}, fmt.Errorf("marshal envelope: %w", err)
	}
	msg := types.FederationMessage{
		Version:   "0.1",
		SourceDID: "did:mcp:client",
		TargetDID: targetDID,
		Type:      msgType,
		Payload:   payload,
		Timestamp: time.Now().UTC(),
	}
	return router.Route(ctx, msg)
}

// ---- decoding helpers ----

func decodeEntityQuery(m map[string]any) (types.EntityQuery, error) {
	var q types.EntityQuery
	b, err := json.Marshal(m)
	if err != nil {
		return q, fmt.Errorf("query: %w", err)
	}
	if err := json.Unmarshal(b, &q); err != nil {
		return q, fmt.Errorf("query: %w", err)
	}
	if q.Type == "" {
		return q, fmt.Errorf("query.type is required")
	}
	return q, nil
}

func decodeFieldDef(m map[string]any) (types.FieldDef, error) {
	var fd types.FieldDef
	b, err := json.Marshal(m)
	if err != nil {
		return fd, fmt.Errorf("field: %w", err)
	}
	if err := json.Unmarshal(b, &fd); err != nil {
		return fd, fmt.Errorf("field: %w", err)
	}
	if fd.Name == "" || fd.Type == "" {
		return fd, fmt.Errorf("field requires name and type")
	}
	return fd, nil
}
