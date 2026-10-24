package probe

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/mozership/probe/jsonutil"
)

// actionResult is what an action returned for a step. An action sends it as
// a map of req, res, rt and status, as the built-in actions return it, and
// it is read once, as it comes back, into the parts of the step's context.
type actionResult struct {
	// Req is the request the action made, as it tells it.
	Req map[string]any
	// Res is the response, with a JSON body, and a JSON stdout, decoded.
	Res map[string]any
	// RT is the time the action took, or none when it did not tell it as a
	// duration.
	RT ResponseTime
	// Status is the action's exit status: 0 for success.
	Status int
}

// readActionResult reads what an action returned for a step.
func readActionResult(m map[string]any) actionResult {
	r := actionResult{Status: parseExitStatus(m["status"])}
	r.Req, _ = m["req"].(map[string]any)
	r.Res, _ = m["res"].(map[string]any)
	if rt, _ := m["rt"].(string); rt != "" {
		if d, err := time.ParseDuration(rt); err == nil {
			r.RT = ResponseTime{Duration: rt, Sec: d.Seconds()}
		}
	}

	if r.Res != nil {
		// A JSON body is read as the value it holds; the text is kept as
		// res.rawbody.
		if body, ok := r.Res["body"].(string); ok && jsonutil.LooksLikeJSON(body) {
			r.Res["rawbody"] = body
			r.Res["body"] = jsonutil.Decode(body)
		}
		// A command that prints a JSON object or array, such as a CLI asked
		// for JSON output, is read as res.json. stdout is kept as it is, for
		// tests that read the text.
		if _, taken := r.Res["json"]; !taken {
			if v, ok := decodeStdoutJSON(r.Res["stdout"]); ok {
				r.Res["json"] = v
			}
		}
	}
	return r
}

// addedResFields returns res, the fields of res an action declares it
// returns, with those readActionResult adds to them: rawbody to one with a
// body, which a JSON body is kept as, and json to one with stdout, which
// stdout of JSON is read as. A field is added only to a res that has the
// field it comes from, so these are all a res can have.
func addedResFields(res []string) []string {
	out := slices.Clone(res)
	if slices.Contains(res, "body") {
		out = append(out, "rawbody")
	}
	if slices.Contains(res, "stdout") {
		out = append(out, "json")
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// parseExitStatus reads the status an action returned: a number, as it is
// sent, or the text "0" for success. Anything else, none included, is a
// failure.
func parseExitStatus(status any) int {
	switch v := status.(type) {
	case int:
		return v
	case int64:
		// Handle integers converted from protobuf float64 by the actionrpc client
		return int(v)
	case float64:
		// Handle JSON numbers which are parsed as float64
		return int(v)
	case string:
		if v == "0" {
			return int(ExitStatusSuccess)
		}
		return int(ExitStatusFailure)
	case ExitStatus:
		return int(v)
	default:
		return int(ExitStatusFailure)
	}
}

// decodeStdoutJSON decodes stdout when the whole of it is one JSON object or
// array, surrounding whitespace aside. Anything else, such as plain text,
// JSON Lines or a scalar, is not decoded.
func decodeStdoutJSON(stdout any) (any, bool) {
	s, ok := stdout.(string)
	if !ok || !jsonutil.LooksLikeJSON(s) || !json.Valid([]byte(s)) {
		return nil, false
	}
	return jsonutil.Decode(s), true
}
