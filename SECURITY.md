# Security policy

dbro holds database credentials and can run SQL, so security reports are taken seriously.

## Reporting a vulnerability

Please report privately through [GitHub's private vulnerability reporting](https://github.com/ExeQue/dbro/security/advisories/new), not in a public issue. Include what you found, how to reproduce it, and the version (`dbro version`).

You will get an answer within a week. Fixes go into a new release, and the advisory is published once users can update.

## Supported versions

Only the latest release gets security fixes.

## Scope

Of particular interest:

- credentials leaking to disk, logs or the MCP server
- agents (MCP) writing to connections that do not allow it, or seeing hidden connections
- `dbro serve`: bypassing the token, host or origin checks
- SSH host key checks being skipped
