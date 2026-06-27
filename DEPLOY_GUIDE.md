# Guia de Deploy e Hospedagem - Sismo

Este documento contém as instruções de hospedagem da arquitetura do projeto Sismo (Landing Page Estática + Worker Monitor em Go).

## 1. Hospedagem da Landing Page (Frontend) no Cloudflare Pages

A landing page (pasta `web/`) não possui chamadas de API próprias e pode ser hospedada 100% gratuitamente via CDN.

### Como criar a hospedagem (Primeiro Deploy):
1. Acesse o painel do [Cloudflare](https://dash.cloudflare.com/).
2. No menu lateral, acesse **Workers & Pages**.
3. Clique em **Create application** (ou "Create") e selecione a aba **Pages**.
4. Clique em **Connect to Git** e autorize sua conta do GitHub.
5. Selecione o repositório `sismo`.
6. Na tela de configurações (Build settings), configure da seguinte forma:
   - **Framework preset:** `None`
   - **Build command:** *(deixe vazio)*
   - **Build output directory:** `web`
7. (Nota: O repositório já contém um arquivo `wrangler.toml` configurado para servir a pasta estática).
8. Clique em **Save and Deploy**. A cada push no GitHub, o site será atualizado automaticamente.

### Como alterar o domínio da página hospedada:
**Opção A: Domínio Próprio (Recomendado - Ex: sismo.com.br)**
1. No painel do Cloudflare, acesse seu projeto `sismo` em *Workers & Pages*.
2. Vá para a aba **Custom Domains**.
3. Clique em **Set up a custom domain**.
4. Insira o domínio comprado e siga as instruções na tela para configurar o DNS onde você o registrou. O HTTPS será configurado de forma automática e gratuita.

**Opção B: Mudar o subdomínio padrão (`.workers.dev`)**
1. No menu principal (fora do projeto), clique em **Workers & Pages** -> **Overview**.
2. Na lateral direita, em **Your subdomain**, clique no botão **Change**.
3. Digite um novo nome e salve. *Aviso: isso altera a URL de base de todos os seus projetos no Cloudflare Pages/Workers.*

### Como desativar a Landing Page temporariamente:
Para tirar o site do ar:
1. Acesse o projeto em *Workers & Pages*.
2. Vá para a aba **Settings**.
3. Role a tela até o final e clique em **Delete project** (botão vermelho).
4. Para colocar de volta no ar, basta refazer a conexão com o Git (Passo 1).

---

## 2. Hospedagem do Monitor em Go (Backend/Worker) no Fly.io

O `sismo-monitor` é um container Docker ultraleve. O [Fly.io](https://fly.io) é uma plataforma PaaS que processa Docker nativamente com um ótimo nível gratuito (Hobby tier).

### Passo a passo para hospedagem:
1. Crie uma conta no [Fly.io](https://fly.io/).
2. Instale a ferramenta de linha de comando oficial (`flyctl` / `fly`) no seu computador.
3. No terminal, abra a raiz do repositório `sismo` e autentique-se:
   ```bash
   fly auth login
   ```
4. Inicialize o aplicativo no Fly (isso gerará um arquivo `fly.toml`):
   ```bash
   fly launch
   ```
   - Responda aos prompts do terminal (escolha uma região próxima, ex: Brasil/EUA).
   - **NÃO** adicione bancos de dados (PostgreSQL/Redis), pois nossa arquitetura não utiliza.
   - Responda **"Não"** (N) quando perguntado se deseja fazer o deploy imediatamente.
5. Cadastre as variáveis de ambiente sensíveis (como os tokens) de forma segura:
   ```bash
   fly secrets set TELEGRAM_BOT_TOKEN="seu-token-aqui"
   ```
6. Realize o deploy para a nuvem:
   ```bash
   fly deploy
   ```
A partir desse momento, o container estará rodando 24 horas por dia na nuvem, publicando no canal do Telegram automaticamente de acordo com as variáveis configuradas.
