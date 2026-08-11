import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { PageHeader } from "@/components/page-header"

describe("PageHeader", () => {
  it("renders title and description", () => {
    render(<PageHeader title="Nodes" description="Manage monitored nodes." />)
    expect(screen.getByText("Nodes")).toBeInTheDocument()
    expect(screen.getByText("Manage monitored nodes.")).toBeInTheDocument()
  })

  it("renders without description", () => {
    render(<PageHeader title="Groups" />)
    expect(screen.getByText("Groups")).toBeInTheDocument()
    expect(screen.queryByText("Manage monitored nodes.")).not.toBeInTheDocument()
  })

  it("renders action children and stacks on mobile", () => {
    render(
      <PageHeader title="Settings">
        <button type="button">Save</button>
      </PageHeader>,
    )
    expect(screen.getByText("Save")).toBeInTheDocument()
  })
})
