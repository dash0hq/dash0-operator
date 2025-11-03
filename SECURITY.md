# Security Policy

## How To Report a Vulnerability

If you think you have found a vulnerability in this repository, please report it to us through coordinated disclosure.

**Please do not report exploitable security vulnerabilities through public issues, discussions, or pull requests.**

Instead, report it using one of the following ways:

* The preferred way of reporting a security issue is the GitHub Security Advisory tab, that is, GitHub's
  ["Report a Vulnerability"](https://github.com/dash0hq/dash0-operator/security/advisories/new) functionality.
* Alternatively, you can disclose security issues anonymously via email by contacting our security team at
  [security@dash0.com](mailto:security@dash0.com).
  See <https://www.dash0.com/policies/vulnerability-disclosure.pdf> for more information.
  Please explicitly state that the security issue is for the Dash0 Kubernetes operator when using this communication
  channel.

Please include as much of the information listed below as you can to help us better understand and resolve the issue:

* the type of issue (e.g., privilege escalation, RBAC permissions, insecure configurations, etc.)
* affected version(s)
* impact of the issue, including how an attacker might exploit the issue
* step-by-step instructions to reproduce the issue
* configuration required to reproduce the issue
* log files that are related to this issue (if possible)
* proof-of-concept or exploit code (if possible)

This information will help us triage your report more quickly.

## Supported Versions

In general, confirmed vulnerabilities will be fixed in the next version of the Dash0 operator.
The decision whether a fix will be ported over to existing older releases is made on a case-by-case basis, and depends
on the severity of the issue, among other factors.
We recommend to always use the latest available [release](https://github.com/dash0hq/dash0-operator/releases) of the
Dash0 operator.
