import os
import sys
import shutil

# Calculate path to template.py
# GOROOT is set by rcc to .../go
# We need .../lib/python3.10/site-packages/robot/htmldata/template.py
goroot = os.environ.get('GOROOT', '')
if not goroot:
    print("GOROOT not set, trying to find site-packages relative to python executable")
    # Fallback logic if needed, but GOROOT should be set in rcc environment
    sys.exit(1)

template_path = os.path.abspath(os.path.join(goroot, '../lib/python3.10/site-packages/robot/htmldata/template.py'))
print(f"Patching {template_path}")

if not os.path.exists(template_path):
    print(f"Error: File not found: {template_path}")
    sys.exit(1)

try:
    with open(template_path, 'r') as f:
        content = f.read()

    # Check if already patched
    patch_marker = "DEBUG: package="
    if patch_marker in content:
        print("Already patched")
    else:
        # Patch the file
        # We want to insert the print at the start of __iter__
        old_code = "    def __iter__(self):"
        new_code = "    def __iter__(self):\n        import sys\n        print(f'DEBUG: package={self._package}, name={self._name}', file=sys.stderr)\n        sys.stderr.flush()"
        
        if old_code in content:
            content = content.replace(old_code, new_code)
            with open(template_path, 'w') as f:
                f.write(content)
            print("Patched successfully")
            
            # Verify patch
            with open(template_path, 'r') as f:
                new_content = f.read()
                if patch_marker in new_content:
                    print("Verification: Patch found in file")
                else:
                    print("Verification: Patch NOT found in file")
        else:
            print("Error: Could not find target code to patch")

    # Clear pycache
    pycache = os.path.join(os.path.dirname(template_path), "__pycache__")
    if os.path.exists(pycache):
        print(f"Removing {pycache}")
        shutil.rmtree(pycache)

except Exception as e:
    print(f"Error patching: {e}")
    import traceback
    traceback.print_exc()
