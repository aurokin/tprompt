# Error Handling

MVP should prefer explicit operational errors over silent fallback behavior.

## Errors that must be clear

### Prompt store errors

- prompt ID not found
- duplicate prompt IDs found
- unreadable prompt file
- invalid frontmatter if parsing is strict enough to reject it

### Environment errors

- not inside tmux when a tmux target is required
- invalid target pane supplied
- configured picker command missing
- daemon socket unavailable

### Delivery errors

- target pane no longer exists
- verification timed out
- tmux command failed
- delivery mode invalid

## Behavioral guidance

- do not silently pick one duplicate ID
- do not silently fall back to a random pane
- do not silently sleep and hope popup state fixed itself
- do not hide daemon failures behind generic “send failed” messages

## Example good error

```text
Unable to deliver prompt 'code-review': target pane %12 no longer exists
```

## Example bad error

```text
Something went wrong
```
