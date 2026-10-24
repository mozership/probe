# Hello Action

The `hello` action does nothing but succeed. It is useful as a placeholder, for a step whose only job is an `echo`, and for trying out expressions.

## Basic Syntax

A hello step takes `echo`, and anything passed in `with` comes back in `res`.

```yaml
steps:
  - name: Report
    uses: hello
    echo: "Checked {{outputs.health.endpoint}}"
```

## Parameters

The action takes no parameters of its own. Whatever is given in `with` is echoed back on `res`, which makes it a convenient way to publish computed values.

```yaml
steps:
  - name: Build a summary
    id: summary
    uses: hello
    with:
      run_id: "{{vars.run_id}}"
      checked_at: "{{now().Format('2006-01-02T15:04:05Z07:00')}}"
    outputs:
      run_id: res.run_id
      checked_at: res.checked_at
```

## Response Object

The step always succeeds, and whatever was passed in comes back unchanged. A step without a `test` passes, rather than being shown as untested, and `probe check` does not warn of it.

| Property | Type | Description |
|----------|------|-------------|
| `res.<key>` | Any | Every key passed in `with` |
| `res.status` | Integer | Always `0` |
| `res.dump` | Boolean | Always `false` |
| `rt.duration` | String | How long the action took, such as `"15µs"` |
| `rt.sec` | Float | How long the action took in seconds |
| `status` | Integer | Always `0` |

`status` and `dump` are set by the action, so a key of either name in `with` does not come back.
