# Teste manual no Kinhank K36

Execute os comandos no Ubuntu, a partir da raiz do repositório. O K36 precisa
estar ligado e acessível por SSH via Tailscale. Substitua o usuário e o endereço
de exemplo pelos que você já usa para conectar ao console.

## 1. Gerar a referência local e compilar para ARM64

```bash
go run ./cmd/stark status
go run ./cmd/stark status --json > ubuntu-status.json
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /tmp/stark-linux-arm64 ./cmd/stark
file /tmp/stark-linux-arm64
```

O comando `file` deve indicar ARM aarch64 e um binário estático. O K36
executa esse binário diretamente, sem instalar Go.

## 2. Preparar o destino em RAM e transferir por SCP

Este bloco procura um diretório gravável e executável em um sistema de arquivos
temporário em RAM. Ele só transfere o binário se encontrar um destino adequado.
O diagnóstico em si usa apenas leitura.

```bash
K36='seu-usuario@ip-ou-nome-tailscale-do-k36'
K36_DIR=$(ssh "$K36" '
  for dir in /tmp /dev/shm "/run/user/$(id -u)"; do
    [ -d "$dir" ] && [ -w "$dir" ] && [ -x "$dir" ] || continue
    [ "$(findmnt -n -o FSTYPE -T "$dir")" = tmpfs ] || continue
    case ",$(findmnt -n -o OPTIONS -T "$dir")," in
      *,noexec,*) continue ;;
    esac
    mktemp -d "$dir/stark.XXXXXX" && exit 0
  done
  echo "Nenhum diretório temporário em RAM permite executar o binário." >&2
  exit 1
') && scp /tmp/stark-linux-arm64 "${K36}:${K36_DIR}/stark"
```

Se o bloco falhar, pare antes de executar os passos seguintes. Os caminhos
temporários em RAM dependem da configuração do K36.

## 3. Executar via SSH e comparar

Depois do SCP bem-sucedido, na mesma sessão do Ubuntu:

```bash
ssh "$K36" "'${K36_DIR}/stark' status"
ssh "$K36" "'${K36_DIR}/stark' status --json" > k36-status.json
diff -u ubuntu-status.json k36-status.json
```

Confira a arquitetura esperada (`AMD64` no Ubuntu de referência e `ARM64`
no K36), a distribuição e o kernel reportados, a RAM total e se o driver do
K36 oferece dados de bateria. Compare o tamanho dos dispositivos de bloco e
consulte o JSON para ver também as partições. O mesmo número de versão do
esquema deve aparecer nos dois relatórios. O `diff` retorna código 1 quando
encontra diferenças. Os dois relatórios ficam no Ubuntu.
