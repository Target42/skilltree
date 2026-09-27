# Skilltree

Firmeninterne Web-App für Wissensgruppen und Skills (Selbsteinschätzung 0–5).  
Details zur Idee: [Idee/Idee.md](Idee/Idee.md).

## Voraussetzungen

- Go 1.22+
- Docker (für PostgreSQL in der Entwicklung)
- PostgreSQL 16 (Produktion oder lokal)

## Schnellstart (Dev)

```powershell
docker compose up -d
go run ./cmd/skilltree
```

Öffnen: http://127.0.0.1:8088 → Login  
(Dev-Postgres liegt auf Host-Port **55432**, damit lokale Postgres-Installationen auf 5432/5433 nicht kollidieren.)

Der HTTP-Server lauscht standardmäßig nur auf **localhost** (`127.0.0.1:8088`) — gedacht für Betrieb hinter nginx auf demselben Host.

Login braucht **Firmenkürzel** + Benutzer + Passwort.

**Selbstregistrierung einer neuen Firma** (`/register`) ist nur aktiv, wenn SMTP konfiguriert ist (`SKILLTREE_SMTP_HOST` + `SKILLTREE_SMTP_FROM`). Nach dem Formular wird eine Bestätigungs-E-Mail mit Link gesendet; erst danach ist die Firma anmeldebereit.

Dev-Mandant `demo` (Passwort jeweils `demo`):

| User | Rolle |
|------|--------|
| `demo` | Plattform-Admin + Tenant-Admin + Löschbeauftragter |
| `alice` | normaler Nutzer |

Mandanten und Tenant-User werden in der UI verwaltet (`/platform/tenants` bzw. `/admin/users`).

Umgebungsvariablen (siehe `.env.example`; lokal wird `.env` beim Start geladen, falls vorhanden):

| Variable | Default |
|----------|---------|
| `SKILLTREE_HTTP_ADDR` | `127.0.0.1:8088` |
| `SKILLTREE_DATABASE_URL` | `postgres://skilltree:skilltree@127.0.0.1:55432/skilltree?sslmode=disable` |
| `SKILLTREE_SESSION_SECRET` | Dev-Default (Warnung beim Start; mit `SKILLTREE_ENV=production` Pflicht) |
| `SKILLTREE_COOKIE_SECURE` | `auto` (`true`/`false` oder TLS/`X-Forwarded-Proto` von Trusted Proxy) |
| `SKILLTREE_TRUSTED_PROXIES` | `127.0.0.1,::1` (nur diese Peers dürfen Forwarded-Header setzen) |
| `SKILLTREE_ENV` | leer (`production` = strikte Secret-Prüfung) |
| `SKILLTREE_SERVICE_NAME` | `skilltree` |
| `SKILLTREE_DISPLAY_NAME` | `Skilltree` |
| `SKILLTREE_SMTP_HOST` | leer (ohne Host+From: kein Versand) |
| `SKILLTREE_SMTP_PORT` | `587` |
| `SKILLTREE_SMTP_USER` | leer |
| `SKILLTREE_SMTP_PASSWORD` | leer |
| `SKILLTREE_SMTP_FROM` | leer (Absender, z. B. `Skilltree <noreply@example.com>`) |
| `SKILLTREE_SMTP_TLS` | `starttls` (`tls` für Port 465, `none` ohne Verschlüsselung) |

Sicherheit (Überblick): Session-Cookie (HMAC, HttpOnly, SameSite=Lax, Secure je nach TLS), CSRF auf allen POSTs, Login-Rate-Limit (10 / 15 min, echte Client-IP hinter Trusted Proxy), neue Passwörter ≥ 8 Zeichen, Request-Body max. 1 MiB.

### Produktion hinter nginx

Das `.deb` legt nginx-Dateien ab (Site wird **nicht** automatisch aktiviert):

| Pfad | Inhalt |
|------|--------|
| `/etc/nginx/conf.d/skilltree-upstream.conf` | `upstream` + Rate-Limit-Zonen (`http {}`, sofort geladen) |
| `/etc/nginx/snippets/skilltree-proxy-params.conf` | Proxy-Header (Trusted-IP) |
| `/etc/nginx/snippets/skilltree-security-headers.conf` | Security-Header |
| `/etc/nginx/sites-available/skilltree` | vHost (HTTP→HTTPS) |

1. Env auf Produktion setzen (`SKILLTREE_ENV=production`, starkes Secret, `127.0.0.1:8088`).
2. In `/etc/nginx/sites-available/skilltree` `server_name` und Zertifikatspfade anpassen.
3. Site aktivieren: `ln -s /etc/nginx/sites-available/skilltree /etc/nginx/sites-enabled/skilltree`
4. `nginx -t && systemctl reload nginx`

Beim Start werden eingebettete SQL-Migrations automatisch angewendet (`migrations/`).

## Build

```powershell
mkdir dist -Force
go build -o dist/skilltree.exe ./cmd/skilltree   # Windows
# go build -o dist/skilltree ./cmd/skilltree     # Linux
```

## Windows-Dienst

Als Administrator:

```powershell
go build -o dist/skilltree.exe ./cmd/skilltree
# optional: Machine-Env für DB-URL setzen
.\dist\skilltree.exe -service install
.\dist\skilltree.exe -service start
```

Oder: `packaging/windows/install-service.ps1`

Steuerung: `-service stop|uninstall|start`

## Linux .deb

Auf Linux (oder Cross-Compile + nfpm):

```bash
GOOS=linux GOARCH=amd64 go build -o skilltree ./cmd/skilltree
docker run --rm -v "$PWD":/work -w /work goreleaser/nfpm package \
  --config packaging/nfpm.yaml --packager deb --target dist/
sudo dpkg -i dist/skilltree_*.deb
# /etc/skilltree/skilltree.env anpassen
sudo systemctl start skilltree
```

Das Paket installiert Binary, systemd-Unit und Beispiel-Env unter `/etc/skilltree/`.

## Struktur

```
cmd/skilltree/     Einstieg + Dienst-Steuerung
internal/app/      Lebenszyklus HTTP + DB
internal/server/   HTTP-Handler
internal/store/    SQL-Zugriff
internal/web/      Templates + Static (embed)
migrations/        goose SQL-Migrations
packaging/         nfpm (.deb), systemd, Windows-Skript
```
