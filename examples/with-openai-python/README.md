# Flexprice + OpenAI (Python)

Demonstrates metering OpenAI API usage via the official Flexprice Python SDK.

Setup
1. Python 3.10+
2. Create and activate a virtualenv
   - python -m venv .venv && source .venv/bin/activate  # Windows: .venv\Scripts\activate
3. cp .env.example .env and fill keys
4. pip install -r requirements.txt
5. python app.py

What it does
- Calls OpenAI Chat Completions (short prompt)
- Reports token usage (units) to Flexprice via Events API
- Prints a rolling summary from recent events

Security
- Never commit real API keys. Use the provided .env.example template and keep your .env local.

Notes
- The example reports units = total_tokens returned by OpenAI for each completion.
- You can change the model and prompt to match your use case.
