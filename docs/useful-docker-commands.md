# Some commands I used to debug the Ryuk mess

```powershell
    docker run --name ryuk-debug -v /var/run/docker.sock:/var/run/docker.sock -p 8080 testcontainers/ryuk:0.14.0

    docker events --filter type=container --format "{{.Time}} {{.Action}} {{.Actor.Attributes.image}} exit={{.Actor.Attributes.exitCode}}"
```
