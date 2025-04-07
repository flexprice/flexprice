#!/usr/bin/env python3
"""
Post-processing script for the FlexPrice Python SDK.
This script adds asynchronous functionality to the events_post method.
"""

import os
import re
import logging
from pathlib import Path

# Configure logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')
logger = logging.getLogger(__name__)

# Paths - adjusted for new script location
SCRIPT_DIR = Path(os.path.dirname(os.path.abspath(__file__)))
API_DIR = SCRIPT_DIR.parent.parent / "python" / "flexprice" / "api"
EVENTS_API_FILE = API_DIR / "events_api.py"
ASYNC_UTILS_FILE = SCRIPT_DIR.parent.parent / "python" / "flexprice" / "async_utils.py"

# Async method implementation - using raw string to avoid linter issues
ASYNC_UTILS_CONTENT = r'''
import asyncio
import threading
import logging
from typing import Dict, Any, Optional, List, Union, Callable
from queue import Queue, Empty
import time
import random
import traceback

logger = logging.getLogger(__name__)

class AsyncEventProcessor:
    """Handler for asynchronous event processing with retries."""
    
    def __init__(self, 
                 max_retries: int = 3, 
                 retry_delay: float = 1.0,
                 max_queue_size: int = 1000,
                 workers: int = 2):
        """
        Initialize the async event processor.
        
        Args:
            max_retries: Maximum number of retry attempts for failed requests
            retry_delay: Base delay between retries (exponential backoff is applied)
            max_queue_size: Maximum number of events to queue
            workers: Number of worker threads to process events
        """
        self.max_retries = max_retries
        self.retry_delay = retry_delay
        self.event_queue = Queue(maxsize=max_queue_size)
        self.workers = workers
        self._worker_threads = []
        self._running = False
        
    def start(self):
        """Start the event processing workers."""
        if self._running:
            return
            
        self._running = True
        for i in range(self.workers):
            thread = threading.Thread(
                target=self._process_queue,
                name=f"event-worker-{i}",
                daemon=True
            )
            thread.start()
            self._worker_threads.append(thread)
        
        logger.info(f"Started {self.workers} async event processor workers")
    
    def stop(self):
        """Stop the event processing."""
        self._running = False
        for thread in self._worker_threads:
            thread.join(timeout=1.0)
        self._worker_threads = []
        
    def _process_queue(self):
        """Worker thread function to process events from the queue."""
        while self._running:
            try:
                event_data = self.event_queue.get(block=True, timeout=0.5)
                if event_data is None:
                    continue
                
                # Validate event_data structure
                if not isinstance(event_data, tuple) or len(event_data) != 3:
                    logger.error(f"Invalid event data format: {event_data}")
                    self.event_queue.task_done()
                    continue
                    
                event, api_instance, callback = event_data
                self._process_event(event, api_instance, callback)
                self.event_queue.task_done()
            except Empty:
                # Empty queue is normal, just continue polling
                pass
            except Exception as e:
                if not isinstance(e, asyncio.TimeoutError):
                    error_details = traceback.format_exc()
                    logger.error(f"Error in event processor: {str(e)}\n{error_details}")
    
    def _process_event(self, event, api_instance, callback):
        """Process a single event with retries."""
        attempt = 0
        last_error = None
        
        while attempt <= self.max_retries:
            try:
                if attempt > 0:
                    # Exponential backoff with jitter
                    delay = self.retry_delay * (2 ** (attempt - 1)) 
                    delay = delay * (0.5 + random.random())
                    time.sleep(delay)
                    
                logger.debug(f"Sending event (attempt {attempt+1}/{self.max_retries+1})")
                result = api_instance.events_post(event=event)
                
                if callback:
                    callback(result=result, error=None, success=True)
                
                logger.info(f"Successfully sent event to FlexPrice API")
                return
            except Exception as e:
                last_error = e
                error_details = traceback.format_exc()
                logger.warning(f"Event delivery failed (attempt {attempt+1}/{self.max_retries+1}): {str(e)}\n{error_details}")
                attempt += 1
                
        # All retries failed
        logger.error(f"Event delivery permanently failed after {self.max_retries+1} attempts: {str(last_error)}")
        if callback:
            callback(result=None, error=last_error, success=False)
    
    def submit_event(self, event, api_instance, callback=None):
        """
        Submit an event for asynchronous processing.
        
        Args:
            event: The event data to process
            api_instance: The API client instance to use for processing
            callback: Optional callback function that receives (result, error, success) params
            
        Returns:
            bool: True if the event was queued, False if the queue is full
        """
        try:
            # Validate inputs
            if event is None:
                logger.error("Cannot submit None event")
                return False
            if api_instance is None:
                logger.error("Cannot submit event with None api_instance")
                return False
                
            self.event_queue.put_nowait((event, api_instance, callback))
            logger.debug(f"Event queued for async processing")
            return True
        except Exception as e:
            logger.error(f"Failed to queue event: {str(e)}\n{traceback.format_exc()}")
            return False

# Global processor instance
_processor = AsyncEventProcessor()
_processor.start()

def submit_event_async(event, api_instance, callback=None):
    """
    Submit an event for asynchronous processing with automatic retries.
    
    Args:
        event: The event data to process
        api_instance: The API client instance to use
        callback: Optional callback function with signature (result, error, success)
        
    Returns:
        bool: True if the event was queued successfully, False otherwise
    """
    return _processor.submit_event(event, api_instance, callback)

# Ensure the processor is stopped when the program exits
import atexit
atexit.register(_processor.stop)
'''

# Async method to add to the events_api.py file - using raw string
ASYNC_METHOD = r'''
    def events_post_async(
        self,
        event,
        callback=None
    ):
        """Send an event asynchronously with automatic retries.
        
        This is a fire-and-forget method that queues the event for processing
        and returns immediately. The event will be processed in a background thread.
        
        Args:
            event: Event data to send
            callback: Optional callback function with signature (result, error, success)
            
        Returns:
            bool: True if the event was queued successfully, False otherwise
            
        Example:
            ```python
            from flexprice import Configuration, ApiClient, EventsApi
            from flexprice.models import DtoIngestEventRequest
            
            # Configure API key authorization
            configuration = Configuration(api_key={'ApiKeyAuth': 'YOUR_API_KEY'})
            
            # Create API client
            api_client = ApiClient(configuration)
            events_api = EventsApi(api_client)
            
            # Create event
            event = DtoIngestEventRequest(
                external_customer_id="customer123",
                event_name="api_call",
                properties={"region": "us-west", "method": "GET"},
                timestamp="2023-01-01T12:00:00Z"
            )
            
            # Send event asynchronously
            events_api.events_post_async(event)
            
            # Or with a callback
            def on_complete(result, error, success):
                if success:
                    print(f"Event sent successfully: {result}")
                else:
                    print(f"Event failed: {error}")
            
            events_api.events_post_async(event, callback=on_complete)
            ```
        """
        from flexprice.async_utils import submit_event_async
        return submit_event_async(event, self, callback)
'''

# Import to add
ASYNC_IMPORT = "import threading"

def add_async_functionality():
    """Add async functionality to the Python SDK."""
    if not EVENTS_API_FILE.exists():
        logger.error(f"Events API file not found: {EVENTS_API_FILE}")
        return False
        
    # 1. Create the async_utils.py file
    logger.info(f"Creating async utils file: {ASYNC_UTILS_FILE}")
    os.makedirs(os.path.dirname(ASYNC_UTILS_FILE), exist_ok=True)
    with open(ASYNC_UTILS_FILE, 'w') as f:
        f.write(ASYNC_UTILS_CONTENT)
    
    # 2. Add the async method to events_api.py
    logger.info(f"Adding async method to events API file: {EVENTS_API_FILE}")
    
    # Read the existing file content
    with open(EVENTS_API_FILE, 'r') as f:
        content = f.read()
    
    # Check if the method already exists
    if 'def events_post_async(' in content:
        logger.info("Async method already exists, skipping")
        return True
    
    # Add the threading import if not present
    if 'import threading' not in content:
        content = re.sub(
            r'(import .*?\n\n)',
            r'\1import threading\n',
            content,
            count=1
        )
    
    # Find a good location to add the async method (after the last events_post method)
    last_method_match = re.search(r'def _events_post_serialize\([\s\S]*?\) -> RequestSerialized:[\s\S]*?_request_auth=_request_auth\n\s*\)', 
                                 content, re.DOTALL)
    
    if not last_method_match:
        # Try more general patterns if the first one fails
        last_method_match = re.search(r'def _events_post_serialize\([\s\S]*?\n\s*\)\n', 
                                     content, re.DOTALL)
    
    if not last_method_match:
        # Try another pattern if the first ones fail
        last_method_match = re.search(r'def events_post_without_preload_content\([\s\S]*?return response_data\.response', 
                                    content, re.DOTALL)
    
    if not last_method_match:
        # As a last resort, try to find any events_post method
        last_method_match = re.search(r'def events_post\([\s\S]*?return[\s\S]*?data', 
                                    content, re.DOTALL)
    
    if last_method_match:
        insert_position = last_method_match.end()
        updated_content = (
            content[:insert_position] + 
            "\n\n" + ASYNC_METHOD + 
            content[insert_position:]
        )
        
        # Write the updated content back to the file
        with open(EVENTS_API_FILE, 'w') as f:
            f.write(updated_content)
            
        logger.info("Async method added successfully")
        return True
    else:
        logger.error("Could not find a suitable location to add the async method")
        return False

def main():
    """Main function to add async functionality to the Python SDK."""
    logger.info("Starting post-processing to add async functionality")
    if add_async_functionality():
        logger.info("Post-processing completed successfully")
    else:
        logger.error("Post-processing failed")
        exit(1)

if __name__ == "__main__":
    main() 