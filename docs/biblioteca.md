# Inventário de ROMs

```bash
stark library
stark library snes
stark library --json
stark library snes --json
```

A CLI procura `/roms`, `/roms2`, montagens com nomes como `roms3` e pastas
`roms` ou `roms2` diretamente sob montagens ligadas a dispositivos de bloco.
Cada pasta de primeiro nível é tratada como um sistema, com exceção de
diretórios auxiliares conhecidos, como `tools` e `bios`. A varredura desce às
subpastas de cada sistema. O filtro aceita o nome de qualquer sistema encontrado
e mantém a contagem feita no inventário completo.

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

O reconhecimento inicial de jogos cobre arquivos SNES em formatos
`.sfc`, `.smc`, `.swc` e `.fig`. Extensões e nomes reconhecíveis de saves,
estados, imagens, vídeos e metadados também são usados em outros sistemas.
Um `.zip` ou `.7z` na biblioteca SNES conta como um jogo quando seu
índice contém pelo menos um arquivo com uma dessas extensões. O nome-base
do pacote serve para as associações. A leitura consulta apenas o índice,
sem extrair ou validar o conteúdo das entradas.

O leitor 7z aceita índices simples e índices comprimidos com LZMA ou
LZMA2. Os limites são de 4 MiB para o índice e sua forma descomprimida,
8 MiB para o dicionário e 16.384 entradas. Um índice ilegível,
criptografado, acima do limite ou com estrutura sem suporte gera uma
pendência. Pacotes inspecionados sem ROM reconhecida e pacotes com
pendência ficam como desconhecidos. Um `.rar` é registrado como pacote
não inspecionado e fica fora da contagem de jogos.

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
