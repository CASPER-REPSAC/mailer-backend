# mailer-backend
## Example Config
```yaml
server:
  address: ":8000"

oidc:
  provider_url: "!!!"
  client_id: "!!!"
  client_secret: "!!!"
  redirect_url: "!!!"
  scopes:
    - "openid"
    - "profile"
    - "email"

authentik:
  base_url: "!!!"
  api_token: "Hide Me!"

smtp:
  host: "smtp.gmail.com"
  port: 587
  username: "casper.cwnu@gmail.com"
  password: "asdf"
  from: "casper.cwnu@gmail.com"

templates:
  email: "./templates/email"
```

## Example Docker compose
```yaml
services:
  backend:
    image: ghcr.io/casper-repsac/mailer-backend:latest
    volumes:
      - ./config.yaml:/app/config/config.yaml
      - ./templates:/app/templates
    environment:
      - SESSION_KEY=!!! # Random key
  frontend:
    image: ghcr.io/casper-repsac/mailer-frontend:latest
    ports:
      - "1086:3000"
```