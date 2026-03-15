## 0.3.28 (2026-03-15)

### Fixes

- prevent space delimiter from splitting multi-word strings

## 0.3.27 (2026-03-14)

### Features

- add array parsing support for environment variables

### Fixes

- address PR review feedback for array parsing

## 0.3.26 (2026-02-14)

### Fixes

- handle nil pointer targets and root namespace struct requests

## 0.3.25 (2026-02-14)

### Features

- add ROOT_NS constant and lookup logic

## 0.3.24 (2026-01-13)

### Fixes

- update dependencies

## 0.3.23 (2026-01-07)

### Features

- add configuration key description management

### Fixes

- ensure atomic rollback on sync failure and propagate watch errors
- add mutex to ensure atomic Set operations with sync
- Add descriptions support and increase code coverage
