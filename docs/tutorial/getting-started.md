# Getting started with paperless

In this tutorial you install `paperless`, read some of your Paperless-ngx data with it, and then let it make one change in a container that exists only for this exercise. It takes about ten minutes. You need a terminal and macOS, Linux and Windows; amd64 and arm64.

## Install the CLI

```sh
brew install Bam679/tap/paperless
paperless version
```

## Connect it to your server

Create an API token in Paperless (Profile > API Auth Token). On macOS, store it in the Keychain so it never appears in your shell history; `security` asks for the token:

```sh
export PAPERLESS_URL=https://paperless.example.com
security add-generic-password -s paperless-cli -a "$PAPERLESS_URL" -w
paperless doctor
```

`doctor` prints the server URL, where the token came from and whether the server answered. On Linux or Windows, set `PAPERLESS_TOKEN` instead of using the Keychain.

## Read your archive

```sh
paperless documents --limit 5
paperless search "insurance" --limit 5
paperless get <id> --content --max-chars 1000
```

You see your newest documents, the best matches for "insurance", and the start of one document's OCR text.

## Try to change something

```sh
paperless tag-create agent-tutorial --dry-run
```

The command exits with code 4: without a policy file the CLI only reads.

## Allow writes for one tag

```sh
mkdir -p ~/.config/agent-cli
cat > ~/.config/agent-cli/paperless.json <<'JSON'
{
  "version": 1,
  "operations": { "allow": ["read", "upload", "tag-create", "delete"] },
  "scopes": { "write": { "allow": ["tag:agent-tutorial"] } }
}
JSON
chmod 600 ~/.config/agent-cli/paperless.json
```

## File a document

```sh
paperless tag-create agent-tutorial --confirm
printf 'Agent tutorial test document\n' > tutorial.txt
paperless upload tutorial.txt --tag agent-tutorial --wait
```

The `upload` command exits with code 5 and shows what it would upload. Run it with `--confirm`; `--wait` waits until Paperless has processed the file and prints the new document id:

```sh
paperless upload tutorial.txt --tag agent-tutorial --wait --confirm
paperless documents --tag agent-tutorial
```

## Clean up

```sh
paperless delete <id> --confirm
rm tutorial.txt ~/.config/agent-cli/paperless.json
```

The tag stays in Paperless; delete it in the web interface if you don't need it.

## What you have done

You installed `paperless` and read data with it. You saw that it refuses to write until a policy allows it, previewed a change with `--dry-run`, and applied it with `--confirm`.

Next steps:

- [How to configure permissions](../how-to/configure-permissions.md) when you want an agent to use the CLI
- [CLI reference](../reference/cli.md) for every command and flag
