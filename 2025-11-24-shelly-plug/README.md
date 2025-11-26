# Shelly plug power measurements

Goal: estimate mean power usage for whatever's currently plugged into
the Shelly Plus Plug one evening this November.

Path is reasonably simple:

```
plug -> UDP listener (on existing raspberry pi) -> log files -> laptop
```

`Makefile` has the entry points of note. UDP listener and logging is
handled by s6.
