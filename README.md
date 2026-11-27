# Stark Manager 🚀

A game library and storage management project for
Linux-based retro handheld consoles.

O Stark Manager foi criado por Will Stark (O Mestre da Impressão 3D).

## 🎯 Project Goals

Create an intuitive tool to manage games, storage
devices, game saves and backups.

## 🛠️ Planned Features

- Unified game library across multiple SD cards
- Automatic bind mounts
- Individual and batch game transfers
- Duplicate game detection
- Save file management and backups
- PortMaster game management
- PC backup comparison and synchronization
- Storage usage analysis
- Safe file operations with integrity verification
- Handheld-friendly interface

## 📱 Target Systems

- ArkOS
- dArkOS
- Compatible Linux-based handheld systems

## 📌 Development Status

A v0.1.1 inclui uma CLI de diagnóstico local para Linux.
Ela lê informações do sistema, RAM, baterias e dispositivos de bloco
e apresenta um resumo em português ou um relatório JSON versionado.

## Executar o diagnóstico

Na raiz do repositório, com Go 1.22 ou posterior:

```bash
go run ./cmd/stark status
go run ./cmd/stark status --json
go run ./cmd/stark storage
go run ./cmd/stark storage --json
go run ./cmd/stark library
go run ./cmd/stark library snes --json
go test ./...
go vet ./...
```

O [guia de diagnóstico](docs/diagnostico.md) descreve o relatório,
as fontes consultadas e os principais arquivos do projeto. Veja também o
[roteiro de teste no Kinhank K36](docs/teste-k36.md), com compilação ARM64,
transferência por SCP e execução por SSH.

O [guia da biblioteca de ROMs](docs/biblioteca.md) descreve a varredura,
a classificação inicial e os limites do inventário.

## 👨‍💻 Developer

Will Stark

## 💡 Vision

Make managing retro game libraries simple, safe,
flexible and accessible.

## Licença

Copyright (C) 2026 Will Stark (O Mestre da Impressão 3D)

O Stark Manager é licenciado sob a GNU General Public License versão 3
(GPL-3.0-only). O texto integral está no arquivo [LICENSE](LICENSE).
