# Mezô · primeiro restaurante piloto

Atualizado em 06/10/2026. Esta versão permite atendimento pelo QR/NFC, pedido pelo garçom, preparo na cozinha, entrega, conta da mesa e registro de pagamento presencial. A aplicação continua única, com banco e usuário PostgreSQL separados por restaurante no Supabase.

## Implantação

1. Abra https://mezo-sage.vercel.app/admin com o acesso privado da plataforma. Cadastre nome, identificador, quantidade de mesas e responsável do restaurante. O cardápio começa vazio.
2. Cadastre os itens e confira seus preços em reais. O campo posiciona a vírgula automaticamente: digitar 3800 mostra R$ 38,00. Desative um item quando acabar; pedidos anteriores preservam o preço registrado.
3. Crie um acesso individual para cada garçom e cada pessoa da cozinha. Escolha a função correta. O responsável opera as duas áreas e pode cancelar pedidos com justificativa. Peça a troca da senha inicial pelo menu “Seu acesso”.
4. Abra o link “Garçom” e o link “Cozinha” em celulares diferentes. A cozinha também funciona em tablet/computador. Mantenha a tela aberta e a conexão disponível durante o serviço; a atualização na Vercel ocorre a cada cinco segundos. Não há push com o aplicativo fechado.
5. Baixe o QR de cada mesa pelo painel. Grave essa mesma URL na etiqueta NFC. Teste no celular antes de imprimir as placas. O QR da demonstração antiga não identifica um restaurante real.
6. Simule uma rodada completa: chamado do cliente → atendimento → pedido com observação → preparo → pronto → entrega → conferência da conta → recebimento presencial → mesa encerrada → nova abertura sem consumo anterior.
7. Confira recebimentos pelo menu do responsável e exporte CSV. Compare os registros de Pix com o banco, cartão com a maquininha e dinheiro com o caixa. O registro do Mezô é uma confirmação da equipe; não liquida pagamentos nem emite documento fiscal.

## Regras da operação

- Uma pessoa assume cada chamado/preparo. O responsável pode devolver chamados à fila ou concluir o preparo de um pedido assumido por outra pessoa.
- A conta inclui o pedido no envio à cozinha. Cancelar antes da entrega retira o consumo e mantém a justificativa no histórico do banco. Pedidos entregues não possuem estorno nesta versão.
- Pedidos na fila, em preparo ou prontos bloqueiam o fechamento. O total é conferido novamente no servidor, inclusive o serviço opcional de 10%.
- Repetir o mesmo envio não cria outro pedido. Se a conexão cair durante o envio, retome a confirmação do envio salvo na aba; confira os pedidos antes de criar outro.
- Desativar uma pessoa revoga seu acesso. Resolva seus chamados/preparos em andamento antes de desativá-la. Não compartilhe a conta do responsável com toda a equipe.
- O relatório diário usa o fuso de São Paulo e contabiliza contas encerradas. Não representa vendas em aberto, documento fiscal ou conciliação automática do banco.

## Backup e recuperação

O comando incluído usa `pg_dump` em formato customizado para o catálogo e cada banco ativo. Cada arquivo possui SHA-256 no manifesto. Ele exige PostgreSQL 17+ no computador e credencial provisionadora em uma configuração privada; as senhas não aparecem nos argumentos dos processos.

```powershell
go run ./cmd/backup -env .env -postgres-bin 'C:\Program Files\PostgreSQL\17\bin' -out backups -verify-local
```

O exemplo acima restaura e compara o conteúdo em bancos temporários locais. `-verify-local` recusa um host remoto. Para a configuração Supabase existente neste workspace:

```powershell
go run ./cmd/backup -env .env.supabase-test -postgres-bin 'C:\Program Files\PostgreSQL\17\bin' -out ../../work/backups/cloud
```

Apesar do nome desse arquivo local, ele contém a configuração privada do ambiente Supabase e fica excluído do Git. Nunca o compartilhe. Os backups também contêm dados privados, hashes e credenciais cifradas. Guarde-os em armazenamento privado separado da máquina de operação e mantenha o APP_SECRET original em um cofre.

Cada banco tem um snapshot consistente próprio. O catálogo e os outros bancos são copiados sequencialmente: suspenda alterações/cadastros para produzir um conjunto coerente de recuperação. O comando não inclui bancos com provisionamento incompleto, usuários PostgreSQL globais nem dados do banco padrão do Supabase.

Para recuperação de desastre, trabalhe em outra instância: recrie os usuários e bancos gerenciados com os mesmos nomes, reestabeleça suas senhas a partir do catálogo usando o APP_SECRET original, restaure os arquivos com o proprietário correto e remova o acesso PUBLIC. Não restaure por cima de produção ativa. Preserve migrações e manifesto do conjunto. Valide isolamento, autenticação e o ciclo de uma mesa antes de trocar a conexão da aplicação. A restauração automática verificada nesta entrega cobre os dados em bancos locais descartáveis; a recuperação completa de desastre, incluindo usuários/segredos e troca de ambiente, ainda precisa de um ensaio operacional acompanhado.

Defina quem executa e confere o backup, a frequência e o tempo de recuperação necessário antes de atender um restaurante real. O comando foi implementado e testado; não há agendamento, cópia externa ou monitoramento de backup configurados.

## Verificações realizadas

- `go test -v ./...` com PostgreSQL local real e `go vet ./...`.
- `govulncheck ./...`: nenhuma vulnerabilidade no código chamado ou nos pacotes importados. O módulo `golang.org/x/crypto` tem o aviso GO-2026-5932 referente a OpenPGP, que a aplicação não importa; o uso de bcrypt não foi apontado pela análise.
- Integração com Supabase: autenticação, isolamento de bancos/tenants, CSRF, concorrência, persistência, pedido/cozinha/entrega e fechamento.
- Atualização de um banco na migração anterior por duas instâncias simultâneas, usando locks do PostgreSQL.
- Bloqueios de função nas rotas novas e antigas, revogação de acessos desativados/troca de senha, preço definido pelo servidor e rollback de pedido com item indisponível.
- Navegador em largura de celular: criação de pedido, observações recebidas na cozinha, preparo, pronto, entrega, conta, fechamento e expiração do acesso do cliente. Painel e relatório conferidos; JavaScript sem erros no fluxo observado.
- Backup dos bancos locais, restauração em bancos temporários e comparação do conteúdo das tabelas. Backup do catálogo Supabase também gerado.

São verificações de fluxos e cenários concretos, não uma certificação de ausência de falhas. Carga máxima, dispositivos físicos/QR impresso, rede instável em restaurante e recuperação completa em outro provedor ainda precisam ser avaliados no piloto. O detector `go test -race` não foi executado: não há compilador C disponível neste computador. As disputas entre requisições foram testadas com PostgreSQL real.

## Antes da primeira operação comercial

| Pendência | Ação do responsável |
| --- | --- |
| Vercel Hobby | A conta atual é Hobby. Uso comercial exige Pro ou Enterprise pela [política da Vercel](https://vercel.com/docs/limits/fair-use-guidelines). Nenhuma assinatura paga foi contratada nesta entrega. |
| Primeiro restaurante | Confirmar os dados reais, cardápio, preços, equipe e suporte; não usar a Casa do Sol fictícia como operação real. |
| Comercial e privacidade | Definir preço, condições de serviço, atendimento, retenção dos dados e revisar contrato/política de privacidade. |
| Pagamento online | Escolher provedor e contas recebedoras; implementar cobranças, webhooks assinados, idempotência, conciliação e estornos. A versão atual funciona com pagamento presencial. |
| Continuidade | Organizar backup externo recorrente, alertas de indisponibilidade/erros, ensaio de recuperação e conexão alternativa do restaurante. |
| Validação do piloto | Testar QR/NFC físico, vários celulares simultâneos, volume do restaurante e a rotina do turno. Definir os limites de uso e acompanhar consumo/custos do Supabase e da Vercel. |

Uma apresentação comercial deve descrever o que esta versão entrega: atendimento, comunicação com a cozinha e acompanhamento da conta. Não prometer pagamento online, emissão fiscal, estoque, reservas ou operação offline completa.
