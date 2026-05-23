#!/bin/bash
# krapow runner wrapper. Exists solely so macOS's "App Background Activity"
# notification (System Settings → Login Items & Extensions) shows this
# filename — "krapow-runner" — rather than the generic "run.sh" that ships
# inside actions-runner/. Function-wise, exec's the upstream entrypoint;
# WorkingDirectory in the plist is already .../actions-runner.
exec ./run.sh
