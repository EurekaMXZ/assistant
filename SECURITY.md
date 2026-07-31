# Security Policy

## Supported Versions

This project does not currently publish a formal release support matrix. The default branch is the primary maintained target. Deployments based on older commits or image tags may not receive security fixes.

Keep production deployments on a current release or image tag, monitor project announcements, and redeploy promptly when security fixes are published.

## Reporting a Vulnerability

Please report suspected vulnerabilities privately through GitHub's private vulnerability reporting:

<https://github.com/EurekaMXZ/assistant/security/advisories/new>

Do not open a public issue or pull request containing exploit details, credentials, private data, or a proof of concept that would enable exploitation.

If private reporting is unavailable, contact a repository maintainer privately through GitHub and provide only enough information to establish a secure communication channel. Do not disclose vulnerability details in a public issue.

### What to Include

Include as much of the following as is safe to share:

- A concise description of the vulnerability and its security impact.
- The affected component, endpoint, workflow, dependency, configuration, or deployment mode.
- The affected commit, image tag, or version.
- Reproduction steps or a minimal proof of concept.
- Required authentication, permissions, configuration, and environmental assumptions.
- Logs, request IDs, stack traces, or screenshots with credentials and personal data removed.
- A suggested mitigation or patch, if available.

Please redact access tokens, JWTs, passwords, API keys, provider credentials, storage credentials, sandbox credentials, conversation content, attachments, and other personal or confidential data before sending a report.

## Response Process

Maintainers will review private reports, reproduce the issue when possible, assess its impact, and coordinate a fix or mitigation. The report may result in a patch, configuration guidance, a GitHub security advisory, or a release note.

In most cases, you should receive an initial response within one day. If you do not receive a response, email `eurekamxz@gmail.com` and the maintainers will investigate and address the report as soon as possible.

Please allow maintainers reasonable time to investigate and prepare a fix before making the vulnerability public. Reporters may be credited in the advisory or release notes when they request credit and it is safe to do so.

## Deployment Security Notes

This application handles authentication tokens, conversations, attachments, provider credentials, billing data, and optional untrusted command execution. Deployers should at minimum:

- Set `WEB_ORIGIN` to the real HTTPS origin in production.
- Replace all example values and generate strong, unique values for `AUTH_JWT_SECRET`, `PROVIDER_CREDENTIAL_MASTER_KEY`, storage credentials, API keys, and bridge tokens.
- Keep `.env` files, credentials, signing keys, database URLs, and service tokens out of source control and container images.
- Never expose server-only secrets through `NEXT_PUBLIC_*` variables or browser runtime configuration.
- Restrict PostgreSQL, Redis, Kafka, object storage, MCP control planes, and sandbox control planes to trusted networks.
- Treat conversation content, uploaded attachments, generated files, logs, and billing records as sensitive data.
- Keep object storage buckets private and use short-lived presigned URLs where applicable.
- Keep sandbox execution disabled unless it is required. When enabled, apply the documented provider isolation and egress restrictions.
- Bind the Firecracker bridge to a private interface or localhost, protect it with its bridge token, and restrict access to trusted API and Worker processes.
- Keep sandbox internet access disabled by default and explicitly allow only the required outbound destinations.
- Apply operating system, container, database, reverse-proxy, and dependency security updates regularly.

The values in `.env.example` are for local development documentation only. They must not be reused in production.

## Scope and Third-Party Components

Reports affecting the API, frontend, Worker, authentication, billing, object storage integration, MCP integration, sandbox bridge, sandbox agent, deployment configuration, or security-sensitive documentation are in scope.

Vulnerabilities that exist solely in an upstream dependency or external service may need to be reported to that project or provider as well. Please include the upstream component and version when relevant.
