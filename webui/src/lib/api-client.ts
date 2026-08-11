import axios from "axios"
import { emitAuthInvalidated } from "@/lib/auth"

export const api = axios.create({
  baseURL: "/api/v1",
  withCredentials: true,
})

api.interceptors.response.use(
  (response) => {
    const body = response.data
    if (body && typeof body === "object" && "code" in body && "data" in body) {
      response.data = body.data
    }
    return response
  },
  (error) => {
    if (error.response?.status === 401) emitAuthInvalidated()
    const message =
      error.response?.data?.error ||
      error.response?.data?.message ||
      error.message ||
      "Unknown error"
    return Promise.reject(new Error(message))
  },
)
