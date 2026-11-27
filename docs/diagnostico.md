# Diagnóstico local v0.1.2

A CLI coleta um retrato do Linux em execução por meio de leituras em `/proc`,
`/sys` e no arquivo `os-release`. Ela roda sem privilégios administrativos,
dispensa comandos externos e envia o resultado para a saída padrão.

- `stark status` exibe o resumo em português, com tamanhos em MiB, GiB ou TiB.
- `stark status --json` exibe o relatório geral em JSON. A execução sem argumentos
  também emite esse relatório, como na primeira entrega.
- `stark storage` lista discos, partições, destinos de montagem e binds do fstab.
- `stark storage --json` emite o relatório de armazenamento em JSON.

O resumo mostra dispositivos de bloco inteiros pelo nome informado pelo Linux.
Partições, dispositivos loop e zram aparecem no inventário JSON. Os nomes como
`sda` e `sdb` são exibidos sem classificação de mídia, pois o coletor atual
não tem dados para distinguir SSD de HD.

## Arquivos principais

| Arquivo | Função |
| --- | --- |
| `go.mod` | Declara o módulo e o requisito de Go 1.22 ou posterior. |
| `cmd/stark/main.go` | Processa os argumentos, executa a coleta e seleciona a saída. |
| `internal/diagnostic/report.go` | Define o contrato JSON versionado. |
| `internal/diagnostic/linux.go` | Lê as fontes do Linux e converte os dados para o relatório. Recebe um `fs.FS` para permitir testes com dados simulados. |
| `internal/presentation/status.go` | Exibe o resumo a partir do relatório, sem coletar novos dados. |
| `internal/presentation/status_test.go` | Testa o resumo, as unidades, os filtros e a exibição de dados ausentes. |
| `internal/diagnostic/storage*.go` | Coleta o inventário detalhado e compara as montagens com o fstab. |
| `internal/presentation/storage.go` | Agrupa partições, destinos e configurações para leitura no terminal. |
| `internal/diagnostic/linux_test.go` | Cobre a coleta, entradas incompletas e a distinção entre falta de dados e inventário vazio. |

## Formato do relatório

`schema_version` começa em `1`. Cada seção contém `status` e pode trazer
`data` e uma lista textual de `issues`.

| Estado | Significado |
| --- | --- |
| `ok` | A coleta detectou zero ou mais entradas sem registrar problemas. |
| `partial` | Há dados aproveitáveis ou um inventário iniciado, com problemas registrados em `issues`. |
| `unavailable` | Falhou a leitura da fonte principal da seção. O campo `data` é omitido. |

Valores desconhecidos são omitidos. Campos numéricos e booleanos preservam
zero e `false`. Uma lista vazia com estado `ok` indica que a fonte estava
acessível e nenhuma entrada correspondente foi encontrada.

| Seção | Dados e fontes |
| --- | --- |
| `system` | Hostname e versão do kernel em `/proc/sys/kernel`, identificação da distribuição em `/etc/os-release` (com fallback para `/usr/lib/os-release`) e modelo opcional via árvore de dispositivos ou DMI. `architecture` indica a arquitetura do binário em execução. |
| `memory` | Total e disponível em bytes, a partir de `MemTotal` e `MemAvailable` em `/proc/meminfo`. |
| `batteries` | Fontes do tipo `Battery` em `/sys/class/power_supply`. Inclui nome, presença quando informada, estado e percentual de carga quando legíveis. |
| `storage` | Dispositivos de bloco visíveis em `/sys/class/block`, incluindo dispositivos virtuais e partições. Inclui tamanho em bytes e a indicação de somente leitura do dispositivo de bloco. |

A CLI emite relatórios parciais normalmente. Uma falha ao emitir o relatório
resulta em um código de saída diferente de zero.

## Relatório de armazenamento

O relatório de `storage --json` tem sua própria `schema_version: 1`. As seções
`devices`, `mounts` e `configured_binds` usam os mesmos estados de disponibilidade
descritos acima. Os dispositivos são ordenados por nome, as montagens por destino
e ID, e os binds seguem a ordem das linhas no fstab.

- `devices` inclui partições desmontadas. O disco pai é identificado pela
  hierarquia do sysfs. `physical_slot` é `null` nesta versão, pois as fontes
  consultadas não comprovam TF1 ou TF2. O modelo reserva nome e evidência
  para quando houver uma identificação verificável.
- `mounts` guarda o destino visível no registro, a raiz dentro do sistema de
  arquivos e o identificador `major:minor`, usado para vincular o registro a
  um dispositivo ou partição. Alguns sistemas de arquivos usam identificadores
  que não correspondem ao sysfs. Nesses casos o vínculo fica em aberto.
  `mount_source` é o campo bruto do kernel com escapes decodificados. Ele
  pode conter o nome de um dispositivo em vez do caminho original de um bind.
  Uma raiz diferente de `/` indica uma subárvore observada e também pode
  representar um subvolume.
- `configured_binds` inclui `bind`, `rbind` e a presença de `noauto`. A
  comparação considera a montagem visível no caminho e o sistema de
  arquivos e raiz esperados para a origem configurada.

| Estado do bind | Significado |
| --- | --- |
| `compatible` | O dispositivo do sistema de arquivos, o tipo e a raiz batem com a configuração. Isso indica compatibilidade com o estado atual. |
| `not_observed` | O destino verificável está sem uma montagem visível própria. |
| `different_origin` | A montagem visível usa outro sistema de arquivos ou outra raiz. |
| `indeterminate` | As fontes, os caminhos ou a topologia não permitem uma conclusão segura. |

Caminhos com links simbólicos ou automontagens não são seguidos na verificação.
Quando há submontagens em um `rbind`, esta versão não confirma a árvore
recursiva. Os registros de montagem descrevem a visão do processo que executa
a CLI e podem mudar durante a coleta.


## Catálogo do EmulationStation

Use `stark systems` para ver os arquivos de configuração encontrados e as
diferenças em relação às bibliotecas montadas. `stark systems --json` inclui
cada entrada na ordem do XML, inclusive nomes repetidos. O campo
`extension` preserva a lista declarada. O conteúdo de `command` é ignorado.

A busca inclui `/etc/emulationstation/es_systems.cfg`, locais de instalação
do sistema e a configuração do usuário que executa o diagnóstico. Quando
há vários candidatos, o relatório indica a ambiguidade e identifica quais
foram lidos. Sem um catálogo válido e inequívoco, a cobertura das pastas
permanece indeterminada.

A comparação inclui as pastas de primeiro nível das bibliotecas descobertas
e verifica diretamente os caminhos configurados, inclusive os aninhados.
Um caminho ausente dentro de uma biblioteca examinada é distinguido de uma
origem indisponível. Os binds confirmados pelo diagnóstico de armazenamento
são apresentados como relações entre os caminhos relevantes.
