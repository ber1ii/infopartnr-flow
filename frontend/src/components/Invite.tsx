import { useState, type FormEvent } from "react"
import { useNavigate, useParams } from "react-router-dom"
import { useMutation, useQuery } from "@tanstack/react-query"
import { api, errMsg, type AuthResponse } from "@/lib/api"
import { useAuth } from "@/lib/Auth"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

export default function Invite() {
  const { token } = useParams()
  const { setSession } = useAuth()
  const navigate = useNavigate()
  const [name, setName] = useState("")
  const [password, setPassword] = useState("")

  const info = useQuery({
    queryKey: ["invite", token],
    queryFn: () => api<{ email: string; client_name: string }>(`/invites/${token}`),
  })
  const accept = useMutation({
    mutationFn: () => api<AuthResponse>(`/invites/${token}/accept`, { body: { name, password } }),
    onSuccess: (r) => {
      setSession(r)
      navigate("/")
    },
  })

  const submit = (e: FormEvent) => {
    e.preventDefault()
    accept.mutate()
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle className="text-xl">
            {info.data ? `Join ${info.data.client_name}` : "Accept invite"}
          </CardTitle>
          <CardDescription>
            {info.isError ? "This invite link is invalid or has expired." : "Create a password to access your dashboard."}
          </CardDescription>
        </CardHeader>
        {info.data && (
          <CardContent>
            <form onSubmit={submit} className="space-y-4">
              <div className="space-y-2">
                <Label>Email</Label>
                <Input value={info.data.email} disabled />
              </div>
              <div className="space-y-2">
                <Label htmlFor="name">Your name</Label>
                <Input id="name" value={name} onChange={(e) => setName(e.target.value)} required />
              </div>
              <div className="space-y-2">
                <Label htmlFor="pw">Password (min 10 characters)</Label>
                <Input id="pw" type="password" minLength={10} maxLength={72} value={password} onChange={(e) => setPassword(e.target.value)} required />
              </div>
              {accept.isError && <p className="text-sm text-destructive">{errMsg(accept.error)}</p>}
              <Button type="submit" className="w-full" disabled={accept.isPending}>
                {accept.isPending ? "Creating account..." : "Create account"}
              </Button>
            </form>
          </CardContent>
        )}
      </Card>
    </div>
  )
}