# Python SDK examples

1. Create a virtual environment and install dependencies:
   ```bash
   python -m venv venv
   source venv/bin/activate   # Windows: venv\Scripts\activate
   pip install -r requirements.txt
   ```
2. Copy `.env.sample` to `.env` and set `FLEXPRICE_API_KEY` (optionally `FLEXPRICE_API_HOST`; include `/v1`, e.g. `us.api.flexprice.io/v1`).
3. Run the sync example: `python example.py`  
   Run the async example: `python async_event_example.py`  
   (From the package root you can run `python examples/example.py` or `python examples/async_event_example.py`.)

**Verified tests:** The same API flows are covered and verified by the integration test suite at **api/tests/python/test_sdk.py**. Use `pip install -r requirements.txt` in that directory (flexprice version pinned there); see [api/tests/README.md](../../tests/README.md).
