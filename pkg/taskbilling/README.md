# Task parameter billing

`task_billing` is a model-level configuration for asynchronous task pricing.
It is opt-in: models without a rule continue to use their channel adaptor's
existing `EstimateBilling` implementation.

Most modes use `ModelPrice` as the base price and produce request-derived
multipliers after channel `param_override` has run. `token_parametric` instead
stores direct USD prices per million provider tokens.

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

### Token price matrix

```json
{
  "version": 1,
  "mode": "token_parametric",
  "token_prices": {
    "paths": ["resolution", "metadata.resolution"],
    "values": {
      "720p": {"standard": 34.5, "reference_video": 21},
      "1080p": {"standard": 38.25, "reference_video": 23.25}
    }
  }
}
```

The relay selects a price from the effective request resolution and its own
reference-video detection. It reserves the equivalent of 250,000 tokens, then
settles against the provider's actual `total_tokens`. The selected unit price,
group ratio, and quota conversion are stored with the task so later setting
changes cannot alter an in-flight bill.

## Additive surcharges

Fixed-price modes can add a per-item surcharge after a free allowance:

```json
{
  "version": 1,
  "mode": "per_second",
  "duration": {
    "paths": ["duration", "seconds", "metadata.duration"],
    "default": 5,
    "round": "ceil"
  },
  "surcharge": {
    "name": "input_images",
    "kind": "item_count",
    "paths": ["conditions", "metadata.conditions", "content", "images", "image", "input_reference"],
    "item_types": ["image", "image_url"],
    "free_count": 5,
    "unit_price": 0.2
  }
}
```

The first non-empty path is used. String arrays count every item; object arrays
count only objects whose `type` matches `item_types` when that filter is
configured. `item_count` also counts a non-empty string as one item. Missing or
empty values count as zero.

The final price is:

```text
(ModelPrice × all dimension multipliers + (count - free_count) × unit_price)
× group ratio
```

Each subtraction has a lower bound of zero. Surcharge counts and prices are
stored with the task billing snapshot and consumption log for auditing.

Rules are validated when saved. Once a rule is configured, its model no longer
uses the channel adaptor's legacy `EstimateBilling` or submit-time adjustment.
