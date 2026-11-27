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
Um `.zip` na biblioteca SNES conta como um jogo se o índice contiver
pelo menos um arquivo com uma dessas extensões. O inventário usa o nome-base
do ZIP para as associações e consulta apenas o índice, sem extrair entradas.
Um ZIP sem ROM reconhecida fica como desconhecido. Se o índice não puder
ser lido, o ZIP também fica como desconhecido e aparece entre as pendências.
Outros formatos compactados e jogos sem regra ficam como desconhecidos.
A associação de um arquivo auxiliar requer uma correspondência única por
nome-base dentro da mesma origem e sistema. O JSON apresenta as demais
associações como `unassociated`.

O inventário usa metadados e nomes dos arquivos, sem abrir o conteúdo das
ROMs. A descoberta usa os caminhos convencionais e as montagens visíveis
ao processo. Bibliotecas fora desses caminhos podem ficar fora do relatório.
