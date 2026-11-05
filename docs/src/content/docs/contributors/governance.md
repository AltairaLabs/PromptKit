---
title: Project Governance
sidebar:
  order: 4
verified:
  commit: 004b9cbbd4d40f770a2e3c76053b600a55189355
  sources:
    - Makefile
    - scripts/setup-branch-protection.sh
---

PromptKit is an open source project developed by [AltairaLabs](https://www.altairalabs.ai). This page describes the governance structure and processes for the PromptKit project.

## Project Goals

PromptKit provides the Go libraries for building LLM applications:

- An SDK for building LLM applications
- A runtime for executing LLM workflows with tool integration
- Shared packages for loading configuration
- An A2A protocol server

The PromptArena testing framework and the PackC pack compiler live in the separate `promptarena` repository.

## Governance Structure

### Maintainers

Maintainers are responsible for the direction, quality and long-term sustainability of the project. They are responsible for:

- Reviewing and merging pull requests
- Managing releases and versioning
- Ensuring code quality and test coverage
- Facilitating community discussions and decisions
- Maintaining project documentation
- Enforcing the Code of Conduct

The current maintainer is the AltairaLabs team.

### Contributors

Contributors are community members who have made valuable contributions to the project through:

- Code contributions (bug fixes, features, improvements)
- Documentation improvements
- Issue reporting and triage
- Community support and engagement
- Testing and feedback

All contributors are recognized and appreciated regardless of the size of their contribution.

### Committers

Committers are trusted contributors who have demonstrated:

- Consistent high-quality contributions over time
- Good understanding of the project architecture and goals
- Collaborative attitude and adherence to the Code of Conduct
- Active participation in project discussions

Committers have additional privileges such as:

- Ability to review and approve pull requests
- Participation in technical discussions and decisions
- Mentoring new contributors

## Decision Making Process

### Consensus Building

The project strives to make decisions through consensus among maintainers and active contributors. For significant changes:

1. Proposal: create a GitHub issue or RFC describing the change.
2. Discussion: allow time for community input.
3. Consensus: maintainers build consensus among stakeholders.
4. Implementation: proceed once consensus is reached.

### Types of Decisions

Day-to-day decisions:

- Bug fixes and minor improvements
- Documentation updates
- Routine maintenance

These can be made by any maintainer or trusted contributor.

Significant decisions:

- New features or major changes
- Breaking changes to APIs
- Changes to project governance
- Addition of new maintainers

These require broader discussion and consensus among maintainers.

## Contribution Process

### Getting Started

1. Read the [Contributing Guide](/contributors/contributing/)
2. Review the [Code of Conduct](/contributors/code-of-conduct/)
3. Check existing issues
4. Start with small contributions to familiarize yourself with the project

### Pull Request Process

The [Contributing Guide](/contributors/contributing/#submitting-changes) describes the full flow:

1. Fork the repository and create a feature branch.
2. Make your changes with tests and documentation.
3. Submit a pull request with a clear description.
4. Participate in the review process.
5. Address feedback and iterate.

### Issue Management

- Use GitHub issues for bug reports, feature requests and questions
- Follow issue templates when available
- Provide clear, detailed information
- Be respectful and constructive in discussions

## Becoming a Maintainer

Maintainer status is earned through sustained contribution and demonstrated commitment to the project. The process involves:

1. Track record: consistent, high-quality contributions over at least 6 months.
2. Trust: demonstrated good judgment and adherence to project values.
3. Nomination: an existing maintainer nominates the candidate.
4. Consensus: current maintainers reach consensus on the nomination.
5. Acceptance: the candidate accepts the responsibilities of maintainership.

### Maintainer Responsibilities

- Technical excellence: ensure code quality and architectural integrity.
- Community building: foster an inclusive and welcoming environment.
- Project stewardship: make decisions in the best interest of the project and community.
- Transparency: communicate decisions and rationale clearly.
- Availability: stay reasonably responsive to community needs.

### Stepping Down

Maintainers may step down at any time. When stepping down:

- Notify other maintainers of the decision
- Transfer any critical responsibilities
- Remove access credentials appropriately
- Maintain confidentiality of any sensitive information

## Code of Conduct Enforcement

The maintainers are responsible for enforcing the [Code of Conduct](/contributors/code-of-conduct/). This includes:

- Responding to reports of misconduct
- Investigating violations
- Taking appropriate corrective action
- Maintaining confidentiality of reports

## Communication Channels

- GitHub issues: bug reports, feature requests, questions, technical discussions
- Pull requests: code review and technical discussions
- Email: private Code of Conduct reports to [conduct@altairalabs.ai](mailto:conduct@altairalabs.ai)

## Project Assets

The following assets are managed by the maintainers:

- GitHub repository and organization settings
- Domain names and websites
- Social media accounts
- Release artifacts and distributions
- Trademark and intellectual property

## Changes to Governance

This governance document may be updated as the project evolves. Changes require:

1. Proposal via GitHub issue or pull request
2. Community discussion period (minimum 2 weeks)
3. Consensus among current maintainers
4. Documentation of the decision and rationale

## Acknowledgments

This governance model is inspired by established open source projects and CNCF practices.
