# Built-in Functions

Probe evaluates expressions with [expr-lang/expr](https://expr-lang.org/) (v1.17). Every function on this page is either a built-in of that language or a function Probe registers on top of it.

## Overview

Expressions appear in two shapes:

- **Template expressions** - a <span v-pre>`{{ ... }}`</span> placeholder inside a string, replaced by the evaluated value.
- **Boolean expressions** - a bare expression, evaluated as a condition.

### Where Expressions Can Be Used

Not every field evaluates expressions, and the ones that do differ in whether they take a template or a boolean expression.

| Level | Field | Template | Boolean | Notes |
|---|---|:-:|:-:|---|
| workflow | `vars` | Yes | - | Global variables. Environment variables are referenced by bare name here |
| job | `name` | Yes | - | Job name |
| job | `skipif` | - | Yes | Skip condition for the job |
| step | `name` | Yes | - | Step name |
| step | `with` | Yes | - | Action arguments |
| step | `test` | - | Yes | Assertion |
| step | `echo` | Yes | - | Report output |
| step | `vars` | Yes | - | Step variables |
| step | `outputs` | Yes | - | Values shared with later steps and jobs |
| step | `skipif` | - | Yes | Skip condition for the step |

### Syntax

Functions are called directly, or chained with the pipe operator. The pipe passes the left-hand value as the **first** argument.

```yaml
vars:
  service: "{{SERVICE_NAME | upper}}"
  slug: "{{replace(lower(SERVICE_NAME), ' ', '-')}}"
  path: "{{BASE_URL | trimSuffix('/')}}/api"
```

Arithmetic and comparison use operators, not functions: `+`, `-`, `*`, `/`, `%`, `**`, `==`, `!=`, `<`, `>`, `&&`, `||`, `!`, `??` (nil coalescing), `in`, `contains`, `startsWith`, `endsWith`, `matches` (regular expression).

```yaml
test: |
  res.code == 200 &&
  res.body.status contains "ok" &&
  rt.sec < 1
```

## Probe Functions

These functions are registered by Probe itself.

### `match_json`

Compares two objects strictly. Every key and value must match on both sides - extra or missing keys make it fail.

A string in `target` written as `/pattern/` is a regular expression that the value in `src` must match. A pattern that does not compile is an error, not a mismatch.

**Syntax:** `match_json(src, target)`
**Returns:** Boolean

```yaml
test: 'match_json(res.body, {"status": "ok", "count": 3})'
```

### `diff_json`

Compares two objects the same way as `match_json` and describes the first difference, checking keys in alphabetical order so the same input always reports the same difference. Returns the string `No diff` when they match.

**Syntax:** `diff_json(src, target)`
**Returns:** String

```yaml
echo: "{{diff_json(res.body, vars.expected)}}"
```

### `parse_json`

Parses a JSON string into an object.

**Syntax:** `parse_json(string)`
**Returns:** Any

```yaml
outputs:
  meta: "{{parse_json(res.body.metadata)}}"

test: parse_json(res.body.metadata).version == "1.0"
```

### `parse_int`

Converts a string or a number to a 64-bit integer. Fails on a string that is not a base-10 integer.

**Syntax:** `parse_int(value)`
**Returns:** Integer

```yaml
test: parse_int(res.headers["Content-Length"]) > 0
```

### `parse_float`

Converts a string to a floating-point number.

**Syntax:** `parse_float(string)`
**Returns:** Float

```yaml
test: parse_float(res.body.score) >= 8.5
```

### `encode_base64`

Encodes a string with standard base64.

**Syntax:** `encode_base64(string)`
**Returns:** String

```yaml
with:
  headers:
    Authorization: "Basic {{encode_base64(vars.user + ':' + vars.pass)}}"
```

### `decode_base64`

Decodes a standard base64 string.

**Syntax:** `decode_base64(string)`
**Returns:** String

```yaml
outputs:
  payload: "{{decode_base64(res.body.data)}}"
```

### `unixtime`

Returns the current time as a Unix timestamp in seconds.

**Syntax:** `unixtime()`
**Returns:** Integer

```yaml
with:
  headers:
    X-Timestamp: "{{unixtime()}}"
```

### `random_int`

Returns a random integer in the range `[0, n)`. `n` must be positive.

**Syntax:** `random_int(n)`
**Returns:** Integer

```yaml
with:
  url: "{{vars.base_url}}/test?seed={{random_int(1000)}}"
```

### `random_str`

Returns a random alphanumeric string of the given length (`[a-zA-Z0-9]`, at most 1000000 characters).

**Syntax:** `random_str(length)`
**Returns:** String

```yaml
vars:
  email: "user-{{random_str(8)}}@example.com"
```

### `file`

Returns the content of a file as a string, as it is: templates in it are not expanded. The path is relative to the working directory, as the paths of actions are. A file larger than 1000000 bytes, a missing file, a directory and anything else that is not a regular file, such as a device, are errors.

**Syntax:** `file(path)`
**Returns:** String

Combined with `parse_json`, a JSON file becomes an object, which is sent as JSON when `content-type` is a JSON type, or compared with a response:

```yaml
with:
  post: /users
  headers:
    content-type: application/json
  body: "{{parse_json(file('fixtures/user.json'))}}"
test: match_json(res.body, parse_json(file('expected/user.json')))
```

### `template`

Expands the `{{ }}` templates in a string, against the same fields as the expression that calls it, such as `vars` and `outputs`, and returns the string. It is what makes a file read by `file` a template:

**Syntax:** `template(string)`
**Returns:** String

```yaml
body: "{{parse_json(template(file('fixtures/user.json.tmpl')))}}"
```

With `fixtures/user.json.tmpl` holding:

```json
{"name": "{{ vars.name }}", "retries": {{ vars.retries + 1 }}}
```

A template is replaced by its value as text, so a string value that may hold a quote is better passed through `toJSON`, which writes it quoted: `{"name": {{ toJSON(vars.name) }}}`. A template in the expanded text that calls `template` again is expanded too, up to 10 levels deep.

## String Functions

These operate on strings, which is what most response bodies and variables are.

| Function | Description |
|---|---|
| `upper(s)` | Uppercase |
| `lower(s)` | Lowercase |
| `trim(s)` / `trim(s, chars)` | Strip whitespace, or the given characters, from both ends |
| `trimPrefix(s, prefix)` | Strip a leading prefix |
| `trimSuffix(s, suffix)` | Strip a trailing suffix |
| `replace(s, old, new)` | Replace every occurrence |
| `split(s, sep)` / `split(s, sep, n)` | Split into an array |
| `splitAfter(s, sep)` | Split, keeping the separator on each element |
| `join(array, sep)` | Join an array into a string |
| `repeat(s, n)` | Repeat a string |
| `indexOf(s, sub)` | Index of the first occurrence, or `-1` |
| `lastIndexOf(s, sub)` | Index of the last occurrence, or `-1` |
| `hasPrefix(s, prefix)` | Prefix test |
| `hasSuffix(s, suffix)` | Suffix test |
| `len(s)` | Length in bytes |

```yaml
vars:
  host: "{{trimPrefix(API_URL, 'https://')}}"
  slug: "{{replace(lower(TITLE), ' ', '-')}}"

test: hasSuffix(res.body.filename, ".json")
```

`contains`, `startsWith`, `endsWith` and `matches` are operators rather than functions:

```yaml
test: |
  res.headers["Content-Type"] contains "application/json" &&
  res.body.id matches "^[0-9a-f]{8}"
```

## Number Functions

These cover rounding, sign and the comparisons that thresholds are written with.

| Function | Description |
|---|---|
| `abs(n)` | Absolute value |
| `ceil(n)` | Round up |
| `floor(n)` | Round down |
| `round(n)` | Round to the nearest integer |
| `max(a, b, ...)` | Largest value |
| `min(a, b, ...)` | Smallest value |
| `sum(array)` | Sum of an array |
| `mean(array)` | Arithmetic mean |
| `median(array)` | Median |
| `bitnot(n)` | Bitwise NOT |

There is no `add` / `sub` / `mul` / `div` / `mod` function - use the operators instead.

```yaml
outputs:
  seconds: "{{round(rt.sec)}}"

test: res.body.id % 2 == 0
```

## Type Conversion Functions

Values read from the environment are strings, so a numeric comparison needs one of these first.

| Function | Description |
|---|---|
| `int(v)` | Convert to an integer |
| `float(v)` | Convert to a float |
| `string(v)` | Convert to a string |
| `type(v)` | Name of the value's type, such as `string` or `int` |

Probe's `parse_int` and `parse_float` differ from `int` and `float` in that they always parse base-10 text and return a 64-bit value.

## Date and Time Functions

These produce and parse time values, and format them for output.

| Function | Description |
|---|---|
| `now()` | Current time as a time value |
| `date(s)` / `date(s, layout)` / `date(s, layout, tz)` | Parse a string into a time value |
| `duration(s)` | Parse a duration such as `"1h30m"` |
| `timezone(name)` | Look up a location such as `"UTC"` or `"Asia/Tokyo"` |

`now()` returns a time value, not a number. Format it with Go layouts through `Format`, or take a Unix timestamp with `Unix()` - or use Probe's `unixtime()`.

```yaml
outputs:
  today: "{{now().Format('2006-01-02')}}"
  started_at: "{{now().Format('2006-01-02T15:04:05Z07:00')}}"
  epoch: "{{unixtime()}}"

test: date(res.body.expires_at) > now()
```

## Array and Map Functions

These inspect and reduce collections, which is what a test on a list of results needs.

| Function | Description |
|---|---|
| `len(v)` | Number of elements |
| `first(array)` / `last(array)` | First or last element |
| `take(array, n)` | First `n` elements |
| `reverse(array)` | Reversed copy |
| `uniq(array)` | Duplicates removed |
| `concat(a, b, ...)` | Concatenate arrays |
| `flatten(array)` | Flatten nested arrays |
| `sort(array)` / `sort(array, "desc")` | Sort |
| `sortBy(array, expr)` | Sort by a computed key |
| `groupBy(array, expr)` | Group into a map by a computed key |
| `filter(array, predicate)` | Elements matching a predicate |
| `map(array, expr)` | Transform each element |
| `find(array, predicate)` / `findLast(array, predicate)` | First or last matching element |
| `findIndex(array, predicate)` / `findLastIndex(array, predicate)` | Index of a matching element |
| `count(array, predicate)` | Number of matching elements |
| `all` / `any` / `one` / `none` | Quantifiers over a predicate |
| `reduce(array, expr, initial)` | Fold an array into a single value |
| `keys(map)` / `values(map)` | Keys or values of a map |
| `get(map, key)` | Value for a key, or `nil` |
| `toPairs(map)` / `fromPairs(array)` | Convert between a map and key/value pairs |

Inside a predicate, `#` is the current element and `#acc` is the accumulator of `reduce`.

```yaml
test: |
  len(res.body.items) > 0 &&
  all(res.body.items, #.status == "active")

outputs:
  names: "{{join(map(res.body.users, #.name), ', ')}}"
  total: "{{reduce(res.body.items, #acc + #.price, 0)}}"
```

## JSON and Encoding Functions

These convert between values and their textual encodings, for request bodies and for output.

| Function | Description |
|---|---|
| `toJSON(v)` | Serialize a value as indented JSON |
| `fromJSON(s)` | Parse a JSON string |
| `toBase64(s)` | Encode with base64 |
| `fromBase64(s)` | Decode base64 |

Note the camel case: there is no `tojson` or `base64`. Probe's `parse_json`, `encode_base64` and `decode_base64` cover the same ground with stricter argument checking and Probe's own error messages.

```yaml
with:
  body: "{{toJSON(outputs.setup.user)}}"

outputs:
  version: "{{fromJSON(res.body).version}}"
```

## Limits

Expression evaluation is bounded for safety:

- An expression may be at most 1000000 characters long.
- Evaluation times out after 5 seconds.
- `parse_json`, `encode_base64`, `decode_base64` and `template` reject an argument longer than 1000000 characters, and `file` a file larger than 1000000 bytes.
- `template` calls nest at most 10 levels deep. Each returns at most 1000000 characters, and the calls nested in one expand at most 10000000 characters in all.

## See Also

- **[YAML Configuration](/reference/yaml-configuration)** - Where each field accepts expressions
- **[Actions Reference](/reference/actions-reference)** - Action parameters and response objects
- **[Expressions and Templates](/guide/concepts/expressions-and-templates)** - Concepts behind the expression language
- **[Testing and Assertions](/guide/concepts/testing-and-assertions)** - Writing `test` expressions
