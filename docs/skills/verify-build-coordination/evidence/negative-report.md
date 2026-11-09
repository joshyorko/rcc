# BuildCoordination

TLC reached 8 states and 11 steps. The chart draws 6 states and 7 arrows.
The chart does not draw 4 steps that stay in one chart state.

## NoStaleFirstPublication breaks
The shortest path has 3 steps:
0. Build requested
1. claim build: First builder owns claim
2. claim expires: First claim expired
3. first builder tries to publish: Stale owner committed an artifact

## Rules that hold
NoDeadlock
