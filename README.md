# Mezô · à sua mesa

Aplicação para atendimento de restaurantes pelo celular. Monólito Go + Echo, PostgreSQL 17, pgx, migrações Goose e painel administrativo com html/template + HTMX. A interface da mesa e da equipe preserva o protótipo existente e usa a API Go e eventos SSE.

## Experimentar neste computador

Execute na pasta deste projeto:

```powershell
.\scripts\dev.ps1
```

O script reconhece o Go portátil preparado neste workspace, usa o PostgreSQL 17 instalado, inicia um cluster próprio em `../../work/mezo-runtime/postgres` na porta 55432 e deixa o servidor em `http://127.0.0.1:4173`. Para outro computador, instale Go 1.27 e PostgreSQL 17 ou informe `-GoExe` e `-PostgresBin`.

Abra `/admin`. E-mails e senhas locais gerados estão em `LOCAL-ACCESS.md`, excluído do Git. O painel oferece os links das mesas, QR codes, acesso da equipe e cadastro de restaurantes, atendentes e cardápio. A conta administrativa inicial é criada uma vez; mudar ADMIN_PASSWORD posteriormente não altera uma conta existente.

O exemplo local Casa do Sol possui 12 mesas, Ana e cardápio fictício. A mesa 8 recebe o consumo de exemplo do protótipo. Esses registros ficam no PostgreSQL; não há chamados falsos pré-carregados. O seed depende de `SEED_DEMO=true` e não é ativado pelo Compose.

Para parar os processos deste projeto:

```powershell
.\scripts\stop.ps1
```

## Separação dos tenants

Uma única aplicação atende todos os restaurantes. Na mesma instância PostgreSQL:

- `mezo_control`: cadastro dos restaurantes, administradores da plataforma e sessões de acesso.
- `mezo_t_<id>`: um banco por restaurante, com equipe, mesas, sessões da mesa, cardápio, consumo, chamados e eventos.
- `mezo_u_<id>`: um usuário PostgreSQL por restaurante, sem superuser, criação de bancos ou usuários.

O servidor procura o banco no catálogo central e confere a sessão e o tenant antes de conectar. A URL, o número da mesa ou um campo enviado pelo navegador não concedem acesso. A conexão pública é removida de todos os bancos gerenciados. Senhas dos bancos dos restaurantes são cifradas com AES-GCM usando APP_SECRET. Senhas humanas usam bcrypt; cookies HttpOnly possuem validade de oito horas, verificação de origem e CSRF nas alterações.

Todos os bancos pertencem à **mesma pasta de dados do PostgreSQL**. O PostgreSQL organiza internamente os arquivos por identificadores em `PGDATA/base`; não há arquivo SQLite ou pasta manual com o nome do restaurante. No Compose, a pasta comum é `./databases`. O banco central compartilha essa instância. Consulte a [organização dos arquivos do PostgreSQL](https://www.postgresql.org/docs/current/storage-file-layout.html).

O catálogo e as permissões são criados pelo provisionador configurado em DATABASE_ADMIN_URL. As consultas usam o usuário limitado do banco central ou o usuário próprio do restaurante. O segredo do provisionador fica no servidor. Pools têm até três conexões; o cache limita a 16 pools e remove pools ociosos quando necessário.

## Fluxos funcionando

- Cliente entra por `/r/<restaurante>/m/<token-aleatorio-da-mesa>`, a mesma URL usada no QR code e etiqueta NFC. A equipe abre e encerra as sessões da mesa.
- Equipe entra por `/r/<restaurante>/equipe`, com login exclusivo daquele restaurante.
- Chamados ficam no banco, entram na fila por idade e atualizam a interface via SSE. Todos os chamados ativos são retornados junto dos 100 atendimentos encerrados mais recentes. Apenas um atendente consegue assumir cada chamado e somente ele pode concluí-lo. Chamados ativos repetidos do mesmo motivo não são duplicados.
- A equipe registra consumo confirmado. Nome e preço são copiados do cardápio no servidor e preservados na conta. O cliente acompanha subtotal, serviço opcional e total calculados em centavos no backend.
- Encerrar a conta presencial invalida o acesso àquela sessão. Reabrir a mesa cria uma conta nova, sem consumo ou chamados da conta anterior.
- Administradores da plataforma cadastram restaurantes, atendentes e itens do cardápio e geram QR codes localmente, sem enviar links para serviços externos.

## Docker e hospedagem

Copie `.env.example` para `.env`. Preencha POSTGRES_PASSWORD e APP_SECRET com segredos aleatórios em hexadecimal e ADMIN_EMAIL/ADMIN_PASSWORD com o acesso inicial. Configure APP_ORIGIN para a origem exata da aplicação; localmente, `http://127.0.0.1:4173`.

```sh
docker compose up -d --build
```

O PostgreSQL não expõe porta externa. Todos os bancos ficam no bind mount `./databases`; a aplicação fica na porta 4173 do loopback. A imagem usa binário único com interface, templates e migrações embutidos. O Compose foi preparado; como o Docker Desktop estava desligado, a execução foi verificada com Go e PostgreSQL nativos.

Para hospedar o backend, use uma VPS com Docker/PostgreSQL e proxy HTTPS, por exemplo Caddy, direcionado para `127.0.0.1:4173`. Configure APP_ORIGIN com o domínio HTTPS antes de imprimir placas: os QR codes usam essa origem e os cookies passam a usar Secure. Uma instância Go atende o SSE desta versão. Para várias réplicas, substitua o hub em memória por eventos compartilhados, por exemplo PostgreSQL LISTEN/NOTIFY.

Faça backup de cada banco e do catálogo com `pg_dump`, dos usuários/permissões com `pg_dumpall --globals-only` e guarde APP_SECRET junto dos segredos de recuperação. Copiar a pasta de um PostgreSQL em execução não substitui um backup consistente. `.env`, LOCAL-ACCESS.md e `databases` estão excluídos do Git e da imagem.

## Vercel

O projeto inclui `vercel.json` com o preset Go e região São Paulo (`gru1`). A aplicação aceita a porta fornecida em `PORT`; não configure `LISTEN_ADDR` na Vercel. O preset compila e executa o servidor HTTP existente com seu comando padrão. Consulte a [documentação oficial do runtime Go](https://vercel.com/docs/functions/runtimes/go).

As dependências estão incluídas em `vendor` para o build não depender do download dos módulos. Depois de atualizar `go.mod`/`go.sum`, execute `go mod vendor` e inclua a atualização no Git.

O build usa `-buildvcs=false` porque o checkout preparado pela Vercel não mantém todos os metadados Git necessários ao stamping do Go. O commit de origem continua identificado pela própria plataforma.

A [prévia pública da mesa 8](https://mezo-demo.vercel.app/#mesa) mostra a interface com dados fictícios. Os arquivos de seu QR code estão em `docs/qr`; a placa identifica que é uma prévia. Esse QR não representa uma mesa de um restaurante em produção.

O PostgreSQL precisa ficar em um serviço externo que permita criar bancos e usuários e conectar diretamente a cada banco. A credencial provisionadora precisa de `CREATEDB`, `CREATEROLE` e permissão para administrar os usuários que cria. PostgreSQL 17 é a versão verificada. Não use a URL do banco local nem envie a pasta `databases` para a Vercel. Em um serviço gerenciado, o provedor administra a pasta física comum do PostgreSQL.

Importe o repositório GitHub e configure estas variáveis no ambiente de produção antes do deploy:

| Variável | Valor |
| --- | --- |
| `DATABASE_ADMIN_URL` | Conexão direta ao PostgreSQL hospedado, com TLS e credencial provisionadora |
| `APP_SECRET` | Segredo aleatório com pelo menos 32 caracteres; preserve o mesmo valor nos próximos deploys |
| `ADMIN_EMAIL` | E-mail do administrador inicial da plataforma |
| `ADMIN_PASSWORD` | Senha inicial de 12 a 72 bytes |
| `APP_ORIGIN` | Origem HTTPS exata e estável da produção, sem caminho |
| `SEED_DEMO` | `false` para começar sem restaurante fictício |

Guarde credenciais como variáveis sensíveis. Para uma demonstração com Casa do Sol, use `SEED_DEMO=true` e defina também `DEMO_STAFF_PASSWORD`; são dados fictícios. A alteração de `ADMIN_PASSWORD` não redefine uma conta já criada. Não compartilhe o banco de produção com deploys de prévia: use outro projeto PostgreSQL e outros segredos para testar mudanças.

Na Vercel, a interface consulta o banco a cada cinco segundos enquanto a página está visível. Isso permite receber chamados entre diferentes instâncias de execução. O modo local continua usando SSE. As conexões ficam em pools por instância; dimensione o limite do PostgreSQL considerando o número de instâncias, restaurantes ativos e conexões administrativas.

Depois de publicar, entre em `/admin`, cadastre o restaurante, abra sua mesa pela equipe e baixe o QR code no painel. Para criar também a placa com a marca Mezô a partir da URL real da mesa:

```sh
go run ./cmd/qr -url 'https://SEU-DOMINIO/r/RESTAURANTE/m/TOKEN-DA-MESA' -table 8 -out qr-mezo-mesa-08
```

O comando gera PNG e SVG para impressão. Use o link da mesa fornecido pelo painel; o token é aleatório. Confirme o acesso em um celular antes de imprimir a placa.

## Verificação

```powershell
$env:MEZO_INTEGRATION = '1'
go test -v ./...
go vet ./...
```

O teste de integração cria dois restaurantes temporários no cluster configurado e remove somente seus próprios bancos/usuários. Verifica isolamento por API e PostgreSQL, autenticação, CSRF, chamados simultâneos, disputa entre atendentes, cálculo de consumo, persistência em uma nova instância Go e encerramento/reabertura sem vazamento. Um teste adicional verifica a criação de restaurante por um provisionador com CREATEDB/CREATEROLE e sem superuser, usando um usuário temporário criado pela fixture local. O teste do hub verifica a separação dos eventos; o teste de configuração verifica PORT e HTTPS na Vercel. Sem MEZO_INTEGRATION=1, os testes PostgreSQL são pulados.

## Estado da integração

O backend funciona localmente. Pix/cartão dependem de provedor, contas recebedoras por restaurante e webhooks; a interface informa que o pagamento online está em configuração e permite chamar a equipe. SSE atualiza páginas abertas. Push com a página fechada ainda não foi integrado. A equipe comunica pedidos à cozinha presencialmente; não há sistema de cozinha nesta etapa.

A publicação privada em Sites permanece uma **demonstração estática**, agora com a marca Mezô. Esse ambiente serve `dist`, não executa o monólito Go nem conecta a este PostgreSQL. A demonstração usa armazenamento local e identifica ações simuladas; o Go insere o marcador de execução real e usa exclusivamente a API.

HTMX 2.0.8 está incluído em `dist/htmx.min.js`; sua licença está em `dist/htmx.LICENSE`.
