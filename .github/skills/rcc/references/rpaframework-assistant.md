# RPA Framework Assistant (Human-in-the-Loop UI)

Use this when you need a lightweight UI for attended or human-in-the-loop automation. The Assistant library provides dialogs with inputs, buttons, and validation.

**Where it lives**
- The Assistant library is part of the RPA Framework, but the Python package for the Assistant UI is published separately as `rpaframework-assistant`.
- Add `rpaframework` and `rpaframework-assistant` to your dependencies when using it from Python.

**Install (RCC conda.yaml)**
```yaml
dependencies:
  - python=3.12.11
  - uv=0.9.28
  - pip:
      - rpaframework==<pin>
      - rpaframework-assistant==<pin>
```

**Version guidance**
- Check PyPI for the latest `rpaframework` and `rpaframework-assistant` versions before pinning.
- As of February 3, 2026: `rpaframework` is `31.1.0` and `rpaframework-assistant` is `5.0.0`.

**Robot Framework example**
```robot
*** Settings ***
Library    RPA.Assistant

*** Tasks ***
Collect Input
    Add heading    Human review required
    Add text input    email    label=Email address
    Add file input    attachment    label=Upload file
    Add submit buttons    buttons=Continue,Cancel
    ${result}=    Run dialog
    Log    ${result}
```

**Python example**
```python
from RPA.Assistant import Assistant

assistant = Assistant()
assistant.add_heading("Human review required")
assistant.add_text_input("email", label="Email address")
assistant.add_file_input("attachment", label="Upload file")
assistant.add_submit_buttons(["Continue", "Cancel"])
result = assistant.run_dialog()
print(result)
```

**Notes**
- Use `Run Dialog` or `Ask User` to open the dialog and block until the user responds.
- Each input needs a unique `name`, and `submit` is reserved for the submit button.
- Results are a DotDict keyed by the input names. Text input returns strings, checkbox returns booleans, file input returns a list of paths.
- If the dialog is closed without submit or there is an internal error, the results may not include a `submit` key.

**References**
- RPA Framework overview: https://rpaframework.org/
- Assistant Python API: https://rpaframework.org/libraries/assistant/python.html
- Assistant library keywords: https://rpaframework.org/libraries/assistant/
- Package index: https://pypi.org/project/rpaframework/
- Package index: https://pypi.org/project/rpaframework-assistant/
