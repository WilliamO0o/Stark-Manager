# Inventário de ROMs

```bash
stark library
stark library snes
stark library ports
stark library --json
stark library snes --json
```

A CLI procura `/roms`, `/roms2`, montagens com nomes como `roms3` e pastas
`roms` ou `roms2` diretamente sob montagens ligadas a dispositivos de bloco.
Cada pasta de primeiro nível é tratada como um sistema, com exceção de
diretórios auxiliares conhecidos, como `tools` e `bios`. Quando identificados,
esses diretórios aparecem no resumo com a indicação de que seus arquivos
ficam fora da contagem. A varredura desce às
subpastas de cada sistema. O filtro aceita o nome de qualquer sistema encontrado
e mantém a contagem feita no inventário completo.

O resumo agrupa os sistemas por família. O JSON inclui `family` e `type`
para cada sistema. Quando disponível, `catalog_entries` mostra o nome,
o nome de exibição, o caminho e a lista de extensões tal como declarados
no EmulationStation. O
[mapeamento mantido pelo Stark](../internal/library/families.tsv) compara
esses três campos. Para pastas sem entrada no catálogo, ele usa somente
nomes exatos de pastas padrão. Uma entrada contraditória ou sem
correspondência fica como não identificada. SuFami Turbo em
`/roms/sufami` e Super NES em `/roms/snes` são registros distintos da
família Nintendo, mesmo com o mesmo ID `snes`. `vmu` aparece na Sega
como conteúdo auxiliar. Engines, ports e plataformas de software têm
grupos próprios, e pastas de configuração e mídia aparecem em Suporte
e mídia.

Os nomes de referência foram conferidos no
[catálogo público dArkOS para RK3326](https://github.com/christianhaitian/dArkOS/blob/master/Emulationstation/es_systems.cfg.rk3326)
e na [lista de pastas padrão](https://github.com/christianhaitian/dArkOS/blob/master/game_systems.txt).

O resultado informa o sistema, os caminhos examinados e a partição quando
identificável. Binds que expõem o mesmo caminho dentro do mesmo sistema de
arquivos têm os arquivos contados uma vez, na primeira ocorrência em ordem
de varredura. Caminhos adicionais ainda podem revelar conteúdo diferente
quando contêm outras montagens. Links simbólicos não são seguidos, e ciclos
e falhas de leitura aparecem como caminhos pendentes. Cartões e partições
sem montagem vinculada aparecem como **não examinados**, com conteúdo
desconhecido.

O resumo conta pastas vazias e pastas com conteúdo sem jogos identificados.
No JSON, `empty_folders` guarda apenas os caminhos listados com sucesso e
sem entradas. `folders_without_games` guarda pastas com conteúdo cuja árvore
foi lida sem encontrar jogos reconhecidos. Arquivos ocultos e subpastas
contam como entradas.

O inventário reconhece GB (`.gb`), GBC (`.gbc`), GBA (`.gba`),
NES/Famicom (`.nes`, `.fds`, `.unf`, `.unif`) e SNES/SFC
(`.sfc`, `.smc`, `.swc`, `.fig`). A extensão indica um formato
provável. O sistema esperado vem do caminho mais específico declarado
no catálogo ou de uma pasta padrão conhecida. O relatório usa as
extensões declaradas para identificar compartilhamentos e para comparar
o arquivo ou pacote com a configuração daquele caminho.

O JSON inclui `rom_checks` com formato, extensões reconhecidas,
sistema esperado, origem, comparação e limitações. Os resultados são
`compatible`, `possible_wrong_folder` e `indeterminate`. Extensões
ambíguas, pacotes com vários sistemas e identificações inconclusivas
ficam como indeterminados. Formatos reconhecidos são contabilizados no
caminho onde foram encontrados.

Um `.zip` ou `.7z` conta como um jogo se o índice listar pelo menos uma
ROM reconhecida. O nome-base do pacote serve para associar arquivos
auxiliares. A inspeção de índices preserva os arquivos no disco e não
valida o conteúdo das ROMs.

O leitor 7z aceita índices simples e índices comprimidos com LZMA ou
LZMA2. Os limites do índice externo são de 4 MiB antes e depois da
descompressão, 8 MiB para o dicionário e 16.384 entradas.

Quando um 7z contém ZIPs, o inventário carrega esses ZIPs em memória e
consulta seus índices. O pacote externo continua contando uma vez,
mesmo com várias versões de uma ROM. A inspeção para no primeiro nível
de ZIPs internos. Por pacote 7z, ela examina até 16 ZIPs internos, com
até 8 MiB por ZIP e 4.096 entradas no conjunto dos ZIPs. Os fluxos
comprimidos e a leitura descomprimida têm limites de 32 MiB cada.
A leitura de ZIPs internos em fluxos 7z simples, incluindo os sólidos,
suporta Copy, LZMA e LZMA2, com dicionário de até 32 MiB.

Índices ilegíveis, criptografia, estruturas sem suporte e limites
excedidos geram pendências. Uma ROM reconhecida ainda conta quando outra
entrada gera pendência. Sem ROM reconhecida, o pacote fica como
desconhecido. Um `.rar` é registrado como pacote não inspecionado e fica
fora da contagem de jogos.

Em SNES, `.cht` indica cheat, `.spc` indica áudio e `.zmv` e `.zm1` a
`.zm9` indicam replays do ZSNES. `.s00`, `.s01` e `.zss` entram como
possíveis estados. O nome exato `gamelist.xml.old` entra como provável
backup de metadados. O JSON lista os caminhos em `unknown_files` e
`uninspected_packages`, além das contagens por categoria.

A associação de um arquivo auxiliar requer uma correspondência única por
nome-base dentro da mesma origem e sistema. O JSON apresenta as demais
associações como `unassociated`.

O inventário usa metadados e nomes dos arquivos, sem abrir o conteúdo das
ROMs. A descoberta usa os caminhos convencionais e as montagens visíveis
ao processo. Bibliotecas fora desses caminhos podem ficar fora do relatório.

## Ports

`stark library ports` resume somente as pastas `ports` encontradas nas
bibliotecas montadas. A seção `ports` do JSON lista os lançadores `.sh`,
pastas relacionadas e arquivos auxiliares de metadados por origem.
Lançadores são contados como arquivos, e aliases por bind são
contabilizados uma vez. Scripts e metadados não são interpretados
para determinar quais jogos estão instalados. Pastas como
`ports_scripts` e `tools` ficam fora do inventário de Ports.

## Atualização e consultas ao cache

```bash
stark library gb
stark library gb --refresh
stark library cache gb
stark library warnings gb
stark library warnings gb --json
```

O filtro seleciona as pastas antes da leitura dos arquivos. Sem filtro,
a atualização percorre as pastas em ordem alfabética. No terminal
interativo, o progresso mostra a pasta atual e o total concluído, e o
resumo de cada pasta aparece assim que ela termina. A visão geral
agrupa as pastas vazias em um contador.

A atualização mostra o último resultado salvo como **em cache** e
relista os arquivos da pasta e de suas subpastas. Quando a identidade
do volume é conhecida, reaproveita classificações se caminho, tamanho,
data de modificação e versão das regras coincidirem. Use `--refresh`
para forçar a releitura dos pacotes no escopo selecionado. Relações entre
jogos, saves e mídias são refeitas com o inventário da atualização.

Cada pasta concluída é salva em um JSON no subdiretório `stark-manager`
de `os.UserCacheDir()`. A gravação substitui o resultado anterior
atomicamente. Uma leitura parcial preserva o último resultado completo.
Sem identidade confiável de volume, o resultado pode ser consultado,
mas a classificação dos arquivos é refeita. Se o cache estiver
indisponível, a leitura ao vivo continua.

`cache` e `warnings` consultam somente o cache, inclusive com os cartões
desconectados. A cobertura inclui apenas as pastas registradas, e cada
resultado mostra a origem e a data da análise. Use `--json` para obter
um único documento sem linhas de progresso.
