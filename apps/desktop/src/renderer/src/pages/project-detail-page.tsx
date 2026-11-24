import { useParams } from "react-router-dom";
import { ProjectDetail } from "@multica/views/projects/components";
import { IssueViewUrlSync } from "@/platform/issue-view-tabs";
import { TerminalMenuItem } from "@/features/lightsoft/terminal-menu-item";

export function ProjectDetailPage() {
  const { id } = useParams<{ id: string }>();

  if (!id) return null;
  return (
    <IssueViewUrlSync scope={{ scope_type: "project", scope_id: id }}>
      <ProjectDetail projectId={id} menuExtra={<TerminalMenuItem projectId={id} />} />
    </IssueViewUrlSync>
  );
}
