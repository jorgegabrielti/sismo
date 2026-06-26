# Sismo - Monitoramento e Alertas de Terremotos em Tempo Real

O **Sismo** é uma plataforma robusta e de escala global desenvolvida para monitorar a atividade sísmica mundial e emitir alertas inteligentes e personalizados em tempo real diretamente para usuários através do Telegram.

---

## 🎯 Objetivo do Projeto

Oferecer informações ágeis, precisas e acessíveis sobre tremores de terra globais. Ao contrário de sistemas de alerta genéricos, o Sismo integra **Filtros de Proximidade Inteligente** (baseados na localização em tempo real do usuário) e **Relatos Comunitários**, permitindo que a população colabore ativamente e se prepare melhor para desastres sísmicos.

---

## 🚀 Principais Funcionalidades

### 1. Alertas Globais Filtrados
- Integração contínua com a API do **USGS (United States Geological Survey)**.
- Transição de escopo regional para monitoramento de escala global, cobrindo todo o planeta com taxas eficientes de tráfego de dados.
- Mapeamento e alerta instantâneo para eventos acima de magnitudes mínimas configuráveis.

### 2. Filtro de Proximidade Geolocalizada (Raio Personalizado)
- Os usuários podem compartilhar sua localização geográfica com o Bot no Telegram.
- Utilização da **Fórmula de Haversine** no backend para medir a distância exata em quilômetros até o epicentro.
- Envio de notificações informando a proximidade relativa do tremor: `📍 Epicentro a 45.2 km de você!`.
- Filtros de distância configuráveis via comando (ex: raio de 150 km) ou desligamento para recebimento global.

### 3. Relatos de Tremores Comunitários ("Did You Feel It?")
- Cada mensagem de alerta do Telegram contém botões interativos integrados (`Sim, senti! 🟢` | `Não senti ⚪`).
- As respostas coletadas são salvas no banco de dados para evitar duplicidade de votos por usuário/sismo.
- Visualização de contadores consolidados na Landing Page em tempo real: `📊 Relatos: X sentiram | Y não sentiram`.

### 4. Visualizações Geográficas Dinâmicas (Mapas)
- **Visualização Direta no Telegram**: Toda notificação enviada pelo Bot anexa automaticamente um mapa estático da área do epicentro com um marcador vermelho no centro, gerado via **Yandex Static Maps API**. Você visualiza o mapa diretamente no balão da mensagem do Telegram, junto com os detalhes técnicos do tremor.
- **Visualização Interativa Avançada**: Cada alerta inclui o link `"Mais detalhes no site da USGS"`. Ao clicar, você é direcionado para a página do evento no portal oficial do USGS, que exibe um mapa interativo 3D em tempo real, permitindo zoom, visualização de placas tectônicas e estações sismográficas próximas.
- **Resiliência de Envio**: O dispatcher de alertas possui um pipeline de resiliência com fallback automático. Caso o servidor de mapas falhe ou esteja inacessível, o bot rebaixa a mensagem para formato de texto simples com teclado interativo para garantir que a notificação de emergência chegue sem atrasos.

### 5. Modo Não Perturbe (DND) e Guias de Emergência
- Ativação ou desativação de alertas silenciosos (sem som de notificação) via comando.
- Guia informativo de preparação civil para instruir a população sobre o que fazer antes, durante e após sismos de grande impacto.

---

## 🛠️ Comandos do Bot no Telegram

Envie estes comandos simples no chat com o bot para configurar suas preferências:

- `/start` : Realiza a inscrição no sistema de alertas e define os limites padrão.
- `/help` : Exibe o guia de uso e a lista de comandos disponíveis.
- `/status` : Exibe seu perfil ativo, magnitude limite, raio de proximidade e modo silencioso.
- `/magnitude <valor>` : Configura a magnitude mínima para alertas (ex: `/magnitude 5.0`).
- `/localizacao` : Instruções sobre como compartilhar a localização.
- `/raio <km>` : Define um raio máximo para alertas em km. Use `0` para desativar e receber alertas de qualquer lugar do mundo (ex: `/raio 200`).
- `/removerlocal` : Desativa o filtro de proximidade e apaga as coordenadas salvas.
- `/listar [periodo] [continente/pais]` : Consulta sismos históricos e recentes correspondentes aos seus critérios (ex: `/listar 12h europa`, `/listar japao 2d`, `/listar usa`).
- `/silencioso` : Alterna o envio de alertas sem som de notificação.
- `/prevencao` (ou `/dicas`) : Exibe diretrizes de proteção civil para terremotos.
- `/stop` : Cancela a inscrição nos alertas do Sismo.

---

## 🏗️ Arquitetura do Sistema

O projeto é escrito em **Go** e estruturado segundo boas práticas de modularização:
- **`cmd/monitor`**: Ponto de entrada do executável principal do monitoramento.
- **`internal/usgs`**: Cliente de consumo, parsing e query da API do USGS.
- **`internal/notifier`**: Mecanismo de enfileiramento e dispatchers (Telegram, Console) com controle de vazão de mensagens (Rate Limiting de 25 msg/s).
- **`internal/db`**: Repositório de persistência SQL do Postgres para armazenar preferências e relatos.
- **`internal/server`**: Servidor Web REST que serve a API de estatísticas e os arquivos estáticos.
- **`web`**: Interface da Landing Page (HTML, lógica JS e estilização em CSS puro).

---

## 📦 Como Executar o Projeto Localmente

### Pré-requisitos
- **Go 1.20+**
- **Docker e Docker Compose**

### 1. Configurar Variáveis de Ambiente
Crie um arquivo `.env` na raiz do diretório com as configurações necessárias:
```env
TELEGRAM_BOT_TOKEN=seu_token_do_bot_telegram
```

### 2. Inicializar os Serviços via Docker Compose
Suba o banco de dados Postgres e a aplicação Sismo com o comando:
```bash
docker compose up --build -d
```
A Landing Page estará disponível localmente na porta: [http://localhost:8085](http://localhost:8085).

### 3. Rodar Testes de Unidade
Você pode executar todos os testes locais com:
```bash
go test ./...
```
