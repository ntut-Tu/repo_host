# Tools architecture

`codehost` is an independent Python library. `codehost-go` is an independent Go
HTTP service. `usage-example` contains Python callers with static demonstration
values and is a sibling of both products.

All server deployment inputs belong to this tools directory:

| Path | Responsibility |
| --- | --- |
| `codehost-go/Dockerfile` | Build/test Go stages and non-root runtime image |
| `deploy/compose.yaml` | Build from the tools root; mount root values.yaml read-only and persist /data |
| `scripts/deploy.sh` | Set build UID/GID and wait for service health |
| `scripts/package_tools_server.sh` | Package an explicit list of deployment inputs |
| `values.example.yaml` | Configuration template without real credentials |
| `.dockerignore` | Limit image build inputs to codehost-go |
| `dist/tools-server.tar.gz` | Generated portable deployment bundle |

The service uses a named data volume and defaults to a loopback host port.
The deployed image contains only the Go executable and its runtime tools.
The usage directory, Python library, IDE files, and private values.yaml are
excluded from the deployment archive and image inputs. A server operator
creates values.yaml at the tools or extracted bundle root before deployment.
