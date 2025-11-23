import os
import sys

os.makedirs("output", exist_ok=True)
with open("output/artifact.txt", "w") as f:
    f.write("artifact content")
print("Artifact created")
