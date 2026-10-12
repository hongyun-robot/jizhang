export default function ChartDataTable({
  label,
  head,
  rows,
}: {
  label: string;
  head: string[];
  rows: (string | number)[][];
}) {
  return (
    <details className="chart-data">
      <summary className="muted small">{label}</summary>
      <table>
        <thead>
          <tr>
            {head.map((h) => (
              <th key={h} scope="col">
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr key={i}>
              {r.map((c, j) =>
                j === 0 ? (
                  <th key={j} scope="row">
                    {c}
                  </th>
                ) : (
                  <td key={j}>{c}</td>
                )
              )}
            </tr>
          ))}
        </tbody>
      </table>
    </details>
  );
}
