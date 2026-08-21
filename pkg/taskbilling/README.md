# Task parameter billing

`task_billing` is a model-level configuration for asynchronous task pricing.
It is opt-in: models without a rule continue to use their channel adaptor's
existing `EstimateBilling` implementation.

`ModelPrice` remains the required base price. The rule produces one or more
positive multipliers from the request body after channel `param_override` has
run.

## Modes

### Per request

```json
{"version":1,"mode":"per_request"}
```

The configured `ModelPrice` is charged once per task.

### Per second

```json
{
  "version": 1,
  "mode": "per_second",
  "duration": {
    "paths": ["duration", "seconds", "metadata.duration"],
    "default": 5,
    "round": "ceil",
    "unit": 1
  }
}
```

`ModelPrice` is the price for `unit` seconds. The first present path is used;
when none is present, `default` is required.

### Parametric

```json
{
  "version": 1,
  "mode": "parametric",
  "dimensions": [
    {
      "name": "duration",
      "kind": "number",
      "paths": ["duration", "seconds"],
      "default": 5,
      "round": "ceil"
    },
    {
      "name": "resolution",
      "kind": "enum",
      "paths": ["resolution", "metadata.resolution"],
      "default": "720p",
      "values": {"480p": 1, "720p": 2.15, "1080p": 5.36}
    }
  ]
}
```

Each number dimension contributes `value / unit` (`unit` defaults to `1`).
Each enum dimension contributes its configured multiplier. The final task price
is the base `ModelPrice` multiplied by every dimension, then by the usual group
and user multipliers.

Rules are validated when saved. Once a rule is configured, its model no longer
uses the channel adaptor's legacy `EstimateBilling` or submit-time adjustment.
