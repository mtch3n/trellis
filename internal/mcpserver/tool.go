package mcpserver

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mtch3n/trellis/internal/platform"
)

var sessionPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

var schemaOptions = &jsonschema.ForOptions{TypeSchemas: map[reflect.Type]*jsonschema.Schema{
	reflect.TypeFor[uuid.UUID](): {Type: "string", Format: "uuid"},
	reflect.TypeFor[[]byte]():    {Type: "string", ContentEncoding: "base64"},
}}

type call struct {
	pr     platform.Principal
	header string
}

func (c call) project(arg string) (string, error) {
	if key := cmp.Or(strings.TrimSpace(arg), strings.TrimSpace(c.header)); key != "" {
		return key, nil
	}
	return "", platform.ErrUsage("unresolved", "this call names no project",
		"pass the project argument, or send the header "+HeaderProject)
}

type toolError struct{ e *platform.Error }

func (t toolError) Error() string {
	raw, err := json.Marshal(t.e)
	if err != nil {
		return t.e.Msg
	}
	return string(raw)
}

func errCode(err error) string {
	if err == nil {
		return platform.CodeOK
	}
	if e, ok := errors.AsType[*platform.Error](err); ok {
		return e.Code
	}
	return "internal"
}

func asToolError(err error) error {
	if e, ok := errors.AsType[*platform.Error](err); ok {
		return toolError{e}
	}
	return toolError{&platform.Error{Code: "internal", Msg: err.Error(), Fix: "retry, or report the error"}}
}

func needs(tool, action, field, value string) error {
	if strings.TrimSpace(value) != "" {
		return nil
	}
	return platform.ErrUsage("missing_field", fmt.Sprintf("%s %s needs the field %s", tool, action, field),
		fmt.Sprintf("call %s with action %s and a %s", tool, action, field))
}

func badAction(tool, action string, actions []string) error {
	return platform.ErrUsage("bad_action", fmt.Sprintf("%s has no action %q in the field action; the actions are %s", tool, action, strings.Join(actions, ", ")),
		fmt.Sprintf("call %s with action one of: %s", tool, strings.Join(actions, ", ")))
}

func callPrincipal(req *mcp.CallToolRequest) (platform.TokenIdentity, call, error) {
	ti := req.Extra.TokenInfo
	if ti == nil {
		return platform.TokenIdentity{}, call{}, platform.ErrPolicy("no_token", "the call carries no agent token", "send Authorization: Bearer <token>")
	}
	id, ok := ti.Extra[identityKey].(platform.TokenIdentity)
	if !ok {
		return platform.TokenIdentity{}, call{}, platform.ErrPolicy("no_token", "the call carries no agent token", "send Authorization: Bearer <token>")
	}
	session := req.Extra.Header.Get(HeaderSession)
	c := call{pr: id.Principal(""), header: req.Extra.Header.Get(HeaderProject)}
	if _, sent := req.Extra.Header[HeaderSession]; sent && !sessionPattern.MatchString(session) {
		return id, c, platform.ErrUsage("bad_session", HeaderSession+" must be 1 to 64 characters from A-Z a-z 0-9 . _ -",
			"send a shorter session name, or omit "+HeaderSession)
	}
	c.pr = id.Principal(session)
	return id, c, nil
}

func argNames(req *mcp.CallToolRequest) (names []string, action string) {
	var args map[string]any
	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return []string{}, ""
	}
	action, _ = args["action"].(string)
	return slices.Sorted(maps.Keys(args)), action
}

func (t *tools) add[In, Out any](s *mcp.Server, tool *mcp.Tool, actions []string, h func(context.Context, call, In) (Out, error)) {
	tool.InputSchema = must(jsonschema.For[In](schemaOptions))
	tool.OutputSchema = must(jsonschema.For[Out](schemaOptions))
	mcp.AddTool(s, tool, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		start := time.Now()
		id, c, err := callPrincipal(req)
		var out Out
		if err == nil {
			out, err = h(ctx, c, in)
		}
		names, action := argNames(req)
		if !slices.Contains(actions, action) {
			action = ""
		}
		inv := platform.Invocation{
			OrgID: id.OrgID, MemberID: id.MemberID, TokenID: id.TokenID, Actor: c.pr.Actor(),
			Tool: tool.Name, Action: action, ArgNames: names, Code: errCode(err), Duration: time.Since(start),
		}
		if logErr := t.p.LogInvocation(context.WithoutCancel(ctx), inv); logErr != nil {
			slog.Error("invocation log", "tool", tool.Name, "err", logErr)
		}
		if err != nil {
			return nil, out, asToolError(err)
		}
		return nil, out, nil
	})
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
