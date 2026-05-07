# Python SDK examples

1. Create a virtual environment and install dependencies:
   ```bash
   python -m venv venv
   source venv/bin/activate   # Windows: venv\Scripts\activate
   pip install -r requirements.txt
   ```
2. Copy `.env.sample` to `.env` and set **`FLEXPRICE_API_KEY`**. Optionally set **`FLEXPRICE_API_HOST`** to a full URL (default: `https://us.api.flexprice.io/v1`).
3. Run the sync example: `python example.py`  
   Run the async example: `python async_event_example.py`  
   (From the package root: `python examples/example.py` or `python examples/async_event_example.py`.)

**Integration tests:** Full API flows are in [api/tests/python/test_sdk.py](../../tests/python/test_sdk.py). Install with `pip install -r requirements.txt` in `api/tests/python`; see [api/tests/README.md](../../tests/README.md).
