# Mesa · Casa do Sol

Interface responsiva, em português, para atendimento de restaurante pelo celular. Casa do Sol é um estabelecimento fictício usado no protótipo.

## Abrir

Sirva a pasta `dist` com qualquer servidor HTTP estático. Exemplo: `python -m http.server 4173 --directory dist`.

## Fluxos

- Cliente: mesa, chamada de garçom com motivo, cardápio com categorias e detalhes, conta com serviço opcional e seleção de pagamento demonstrativo.
- Garçom: fila de chamados, filtros, assumir e concluir atendimento, histórico e visão das mesas.
- Os chamados são salvos no navegador, com sincronização entre abas do mesmo navegador/origem. Não há autenticação, backend, envio de notificações push ou cobrança real.
- O botão de opções permite reiniciar a demonstração. Valores monetários são calculados em centavos.

## Arquivos

- `dist/index.html`: estrutura da página.
- `dist/styles.css`: interface e adaptações responsivas.
- `dist/app.js`: navegação, dados e interações locais.

O cliente e o garçom compartilham dados somente neste navegador. Esta entrega permite avaliar a interface antes das decisões sobre backend e banco.
