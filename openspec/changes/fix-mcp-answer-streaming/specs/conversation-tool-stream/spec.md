## ADDED Requirements

### Requirement: Stream the final answer after bounded tool decisions
The production conversation service SHALL execute at most three structured decision requests and four tool calls, then at most one final model request with streaming enabled and further tool calls disabled. It SHALL forward model text incrementally without replaying a completed answer as simulated streaming. A conversation with no bound tool candidates SHALL retain the ordinary single streaming request.

#### Scenario: Stream after a successful tool call
- **WHEN** a tool decision has completed and the final model begins returning text
- **THEN** the client receives the first text delta before the model completes the final response
- **AND** the final model request includes prior tool call identifiers and tool-role results, stream true and tool choice none

#### Scenario: No tool requested during the first decision
- **WHEN** candidates exist but the decision response proposes no tool call
- **THEN** the service performs one final streaming request and does not show the non-streamed decision content
- **AND** the entire turn uses two model requests

#### Scenario: Stop after the decision limit
- **WHEN** three decision rounds have completed
- **THEN** the service makes no additional decision request and only the final streaming request may follow

#### Scenario: Final stream fails after partial text
- **WHEN** text has reached the client but the model stream is interrupted
- **THEN** the server saves the partial answer and reports a safe error without retrying the model

### Requirement: Display tool activity before answer text
The service SHALL start SSE before executing tools and flush each tool status as it occurs. The page SHALL render the tool name and a Chinese status label before the answer area. The initial meta event MAY have an empty tool list; subsequent tool events SHALL carry the outcomes. Current-turn tool state SHALL survive the final message reload and clear on a new question or conversation switch.

#### Scenario: Running tool and final answer
- **WHEN** a tool is running and no answer text has been generated
- **THEN** the client can already display its name and running status
- **AND** successful, failed, refused or timed-out outcomes remain visible before the answer text

#### Scenario: Switch conversations
- **WHEN** the user switches or creates a conversation
- **THEN** previous-turn tool activity is cleared and not attributed to the new conversation

### Requirement: Recover the conversation list after uncertain creation
The page SHALL prevent simultaneous repeated create submissions. If creation fails, it SHALL read the owned conversation list again, preserve the error and instruct the user to inspect the list before retrying. It SHALL NOT automatically issue another create request or infer that an error guarantees no saved row.

#### Scenario: Create response fails after a row was saved
- **WHEN** the create request returns an error but the server has saved a conversation
- **THEN** the page refreshes the list and lets the user open the saved conversation
- **AND** it does not create another conversation automatically
