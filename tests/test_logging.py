from __future__ import annotations

import json
import logging

from app.core.logging import JsonLogFormatter


def test_json_log_formatter_outputs_structured_record() -> None:
    formatter = JsonLogFormatter()
    record = logging.LogRecord(
        name="proxy_check.test",
        level=logging.INFO,
        pathname=__file__,
        lineno=10,
        msg="probe completed",
        args=(),
        exc_info=None,
    )

    payload = json.loads(formatter.format(record))

    assert payload["level"] == "INFO"
    assert payload["logger"] == "proxy_check.test"
    assert payload["message"] == "probe completed"
    assert "timestamp" in payload
