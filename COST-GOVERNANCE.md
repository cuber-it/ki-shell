# aish — Cost-Governance (Konzept & Fahrplan)

Stand: 2026-06-19. Arbeitstitel **aish** (vormals kish — „KI" ist zu deutsch).

## Vision / USP

aish wird **die bash-kompatible Shell mit eingebauter, *durchgesetzter* Kosten-Governance.**
Markt-Recherche (2026): **kein** Endnutzer-AI-Shell/CLI-Tool (Warp, aichat, mods,
Butterfish, Open Interpreter, Copilot CLI, Amazon Q, …) hat einen konfigurierbaren
**Hard-Stop** bei Token-/Dollar-Limit. Entweder gar nichts, nur Plan-Quotas, oder
(Warp) Enterprise-credit-basiert ohne per-Run-Limits. Echter Hard-Stop existiert nur in
**Gateway/Proxy-Schicht** (LiteLLM, Cloudflare AI Gateway, Portkey) — *vor* dem LLM,
keine Shell. → aish besetzt die Lücke: **Cost-Guard als First-Class-Citizen in der Shell,
die man täglich benutzt.**

Leitsatz (aus `uc_llm_cost`): **„NIEMALS still überschreiten — immer explizit stoppen
oder warnen."** Und **fail-closed**: Budget-Check *vor* dem Call; bei Überschreitung wird
der Call **verweigert**, nicht geloggt-und-trotzdem-gemacht. (Genau hier hat GitHub
Copilot CLI einen dokumentierten Bug, #198005 — das machen wir richtig.)

**Was aish NICHT will** (Recherche-Leitplanken): nicht mit aichat um Provider-Breite
konkurrieren, nicht mit Claude Code/Copilot um Coding-Agent-Qualität, nicht mit Warp um
UX/Cloud. MCP ist Pflicht, aber kein USP. Der **Cost-Guard ist der Hook, nicht die Shell**.

## Architektur (drei Schichten)

1. **Cost-Guard (Kern, neu)** — Go-Port von qatakis `uc_llm_cost`. Pure stdlib, fail-closed.
   - Ebene 1: Hard-Limit pro Run (Token + USD) → `CostLimitExceeded`, Abbruch *vor* Call.
   - Ebene 2: Soft-Limit (80 %) → Warnung, Call läuft.
   - Ebene 3: Sparmode (90 %) → `max_tokens` reduziert / kürzere Prompts.
   - Ebene 4: Tages-/Monatsbudget → gesperrt.
   - Ebene 5: Killswitch → Hard-Stop alles.
   - Audit: jeder Verbrauch + jede Sperre in `~/.kish/cost_audit.jsonl`.
   - Persistenz: `costs.db` (vorhanden) liefert die verbrauchten Zahlen; Budget-Overrides
     in `~/.kish/budget.json` (UI-editierbar).
2. **Provider-Layer (entheinzeln)** — `ki_provider.go` hängt an `heinzel-ai-core-go`
   (`replace => ../heinzel/...` → baut nur auf Cirrus, nicht portabel). Adapter rausziehen:
   schlanker eigener Layer (OpenAI/Anthropic direkt, OpenAI-kompatibel für lokal/LiteLLM).
   **Provider-Breite NICHT selbst pflegen.** Der Cost-Guard hängt *vor* diesem Layer.
3. **Steuerung & Monitoring (Shell + Web)** — siehe unten.

## Steuerung aus der Shell

```
ki:budget                      # aktuelle Limits + Verbrauch + % ausgeschöpft
ki:budget set month 10         # Monatslimit 10 USD
ki:budget set day 2            # Tageslimit 2 USD
ki:budget set run 0.50         # Limit pro Anfrage 0,50 USD
ki:budget set tokens-run 4000  # Token-Deckel pro Anfrage
ki:budget confirm 0.20         # ab 0,20 USD/Anfrage Rückfrage
ki:budget off                  # Limits aus (explizit, mit Warnung)
ki:killswitch on|off           # Hard-Stop für alle KI-Calls
```
Persistiert in `~/.kish/budget.json`. Env/Config als Defaults, `budget.json` als oberste
Schicht (wie `uc_llm_cost.load_overrides`).

## Monitoring

```
ki:costs                       # heute / Monat verbraucht, vs. Limit, verbleibend, Top-Modelle
ki:costs log                   # letzte N Anfragen (Tokens, $, Latenz)
ki:audit                       # Audit-Log inkl. Sperren/Warnungen (vorhanden, erweitern)
```
- **Prompt-Indikator:** bei ≥80 % Monatsbudget dezente Warnung im Prompt; bei Killswitch/Hard-Stop deutlich.
- **Web-UI:** `/api/costs` (vorhanden) → Budget-Panel mit Limits editierbar + Live-Verbrauch.

## Roadmap (Bausteine, in Reihenfolge)

1. **cost_guard.go** (Kern, fail-closed) + `budget.json` + Audit. Unit-Tests für jede Ebene (Failure-Pfade!).
2. **Einhängen vor dem Provider-Call** in `ki_provider.go` (`Query`): Pre-Check → ggf. Abbruch; Post: Verbrauch buchen.
3. **`ki:budget`-Builtin** (Steuerung) + `ki:costs` erweitern (vs. Budget).
4. **Entheinzeln**: heinzel-Adapter raus, schlanker Provider-Layer → baut/läuft überall (auch Acer).
5. **Prompt-Indikator** + **Web-Budget-Panel**.
6. Rebrand kish → **aish** (Binary, Config-Dir `~/.aish`, Docs) — am Ende, wenn Kern steht.

## Tests / Failure-Mode (Pflicht)
Jede Guard-Ebene mit eigenem Test + State-Assertion. Besonders: Pre-Call-Hard-Stop muss
fail-closed sein (bei DB-/Pricing-Fehler → Call verweigern, nicht durchwinken). Kein
„eval { } ; warn; weiter".
