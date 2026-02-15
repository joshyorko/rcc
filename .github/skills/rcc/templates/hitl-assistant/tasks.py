#!/usr/bin/env python3
"""Simple producer/consumer with Assistant UI and custom work item adapter."""

import argparse
import json
from datetime import datetime

from RPA.Assistant import Assistant
from robocorp import workitems


def build_payload(index: int) -> dict:
    return {
        "id": f"item-{index}",
        "repo": "example/repo",
        "summary": "Review the generated report",
        "created_at": datetime.utcnow().isoformat() + "Z",
    }


def run_producer() -> None:
    for i in range(3):
        workitems.outputs.create(payload=build_payload(i))


def run_consumer() -> None:
    for item in workitems.inputs:
        payload = item.payload

        assistant = Assistant()
        assistant.add_heading("Human Review Required")
        assistant.add_text("Please review this item:")
        assistant.add_text(json.dumps(payload, indent=2))
        assistant.add_submit_buttons(["Approve", "Reject"])
        result = assistant.run_dialog()

        decision = result.get("submit", "Reject")
        if decision == "Approve":
            item.done()
        else:
            item.fail(
                exception_type="BUSINESS",
                message="Rejected by reviewer",
            )


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("-t", "--task", required=True, choices=["producer", "consumer"])
    args = parser.parse_args()

    if args.task == "producer":
        run_producer()
    else:
        run_consumer()


if __name__ == "__main__":
    main()
