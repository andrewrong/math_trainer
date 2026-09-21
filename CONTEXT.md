# Learning Compass Context

## Glossary

**Learning record**: The saved evidence of a learner's practice, progress, review, reading, or assignment. A learning record is owned by Learning Compass even when an external agent creates or updates it.

**Automation client**: A trusted AI control-plane process that acts on the family's Learning Compass data through the Agent API. It is not a learner-facing user.

**Agent API**: The token-protected interface through which an automation client discovers and uses Learning Compass capabilities. It presents the same learning operations as the local application without changing their meaning.

**Control token**: The shared credential that authorizes a trusted automation client to use the Agent API. It grants access to all exposed learning records for this single-family deployment.
