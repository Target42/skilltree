# Skilltree

## Idee

Web-Application zur Verwaltung von Wissen in Firmen. Wissen wird als Baum aus **Gruppen** modelliert; Gruppen können wiederum Gruppen enthalten. In den Gruppen hängen **Skills**.

Zu jeder Gruppe und jedem Skill gibt es eine Beschreibung. Bei einem Skill kann man einschätzen, wie gut man darin ist (Skala **0–5**). Keine Angabe ist gleichbedeutend mit „kein Wissen“.

Angaben sind **freiwillig**. Im System darf **jeder alles sehen** – Transparenz statt Zugriffskontrolle auf Inhalte.

Man kann sich auf Gruppen **registrieren (Watch)**. Kommen dort neue Skills hinzu, wird man benachrichtigt und kann die eigenen Fähigkeiten dazu einschätzen. Watch bedeutet nur Benachrichtigung, kein Pflicht-Skillset.

### Domänenregeln

| Thema | Entscheidung |
|--------|----------------|
| Mandanten | Shared-DB mit `tenant_id`; eine Firma = ein Mandant, eigener Wissensbaum |
| Login | Firmen-Slug + Benutzername + Passwort |
| Sichtbarkeit | nach Login alles **im eigenen Mandanten** sichtbar (freiwillig) |
| Bewertung | 0–5; leer = kein Wissen; Levels anderer sichtbar |
| Struktur | Wissensbaum, keine Org-Abbildung; Org kann später eine *Sicht* auf denselben Baum sein |
| Skills | eigenständige Entität, **mehrfach referenzierbar** (n:m Gruppe↔Skill) |
| Watch | nur Notify bei neuen Skills in beobachteten Gruppen |
| Anlegen | jeder (eingeloggt) darf Skills/Gruppen anlegen |
| Löschen Gruppe | Ersteller, nur wenn leer (keine Untergruppen/Skills); sonst Löschantrag |
| Löschen Skill | Ersteller, nur solange keine fremden Bewertungen; sonst Löschantrag |
| Löschanträge | Löschbeauftragte (oder Tenant-Admin) entscheiden |
| User-Verwaltung | Tenant-Admin legt User an/ändert/deaktiviert; Plattform-Admin legt Mandanten an |
| Custom Stylesheets | zurückgestellt |

## Realisierung

### Stack

- **Backend:** Go
- Frontend: serverseitig gerendert (Templates/templ) + HTMX/leichtes JS, als Assets **im Go-Binary eingebettet** (ein Deploy-Artefakt)
- UI: **Gruppen als Baum** (links), **Skills als Tabelle** (rechts) in einer Arbeitsansicht
- **Datenbank:** **PostgreSQL**
- **Schema:** SQL-Migrations (z.B. goose oder golang-migrate), nicht nur implizit im Code
- **Deployment:** Go-Server hinter **nginx**

Begründung Frontend: internes Tool (Formulare, Baum, Bewertungen) – eine eingebettete Template-UI hält Angriffsfläche und Deploy einfach; eine SPA lohnt erst bei deutlich reicherer Interaktion.

### Datenmodell (Skizze)

- `tenants` – Firma (Slug, Name, aktiv)
- `users` – Login (Passwort-Hash), `tenant_id`, Tenant-Admin, Plattform-Admin, Löschbeauftragter, aktiv
- `groups` – Baum (`parent_id` nullable), `tenant_id`, Name, Beschreibung, `created_by`
- `skills` – Name, Beschreibung, `tenant_id`, `created_by` (eigenständig)
- `group_skills` – n:m-Zuordnung Gruppe↔Skill
- `skill_ratings` – `(user_id, skill_id, level 0–5)`, unique je User/Skill
- `group_watches` – Watch/Notify
- `notifications` – In-App-Hinweise
- `deletion_requests` – beantragte Löschungen (pending/approved/rejected), `tenant_id`

Notify bei neuen Skills in beobachteten Gruppen: zuerst In-App; E-Mail später optional.

### Architektur

```
Browser → nginx → Go (HTTP + Templates + Static)
                      ↓
                 PostgreSQL (SQL-Migrations)
```

### Auslieferung

- **Linux:** `.deb` via nfpm (`packaging/nfpm.yaml`) inkl. systemd-Unit (`skilltree.service`) und Env unter `/etc/skilltree/`
- **Windows:** ein Binary als Dienst (`skilltree.exe -service install|start|stop|uninstall`, kardianos/service)
- Dev-Datenbank: `docker compose up -d` (PostgreSQL 16)

## Offen / später

- SSO / zentrale User-Provisionierung
- Watch-UI und E-Mail-Notify
- Org-Sicht auf denselben Skillbaum
- Custom Stylesheets / Branding
- Admin-Werkzeuge (Duplikate zusammenführen)
