import { useQuery } from "@tanstack/react-query";
import { apiGet, User } from "../api";
import { Badge, Empty, PageHeader, PanelBox, TableSkeleton } from "../ui";

export function UsersPage() {
  const { data, isLoading } = useQuery({ queryKey: ["users"], queryFn: () => apiGet<User[]>("/users") });
  return (
    <>
      <PageHeader title="Users" sub="Panel operators and their roles" />
      <PanelBox>
        {isLoading ? <TableSkeleton cols={3} /> : !data?.length ? <Empty>No users.</Empty> : (
          <table>
            <thead><tr><th>Email</th><th>Role</th><th>Status</th></tr></thead>
            <tbody>
              {data.map((u) => (
                <tr key={u.id}>
                  <td style={{ fontWeight: 550 }}>{u.email}</td>
                  <td><Badge status={u.role} /></td>
                  <td><Badge status={u.disabled ? "disabled" : "active"} /></td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </PanelBox>
    </>
  );
}
